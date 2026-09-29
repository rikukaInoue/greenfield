# 7.2b(#175): 早期判定の再演に使う追加リソース。
#   - テストリスナー(:8081) + ルール: TEST_TRAFFIC_SHIFT で green にテストトラフィックを流す口
#   - フック Lambda: テストリスナー越しに healthz + **書き込み**を検証し hookStatus を返す
#   - ALB 5xx アラーム: カナリア窓の自動ロールバック判定
#
# lifecycleHooks / alarms / testListenerRule の**サービスへの取り付けは aws cli で行う**。
# 7.2 の実測で provider(aws ~>6.x) が無効なデプロイ設定を黙って落とす(fail open)ことが
# 分かっているため、取り付けの正は API に置き、効いたことはデプロイ挙動で確認する。

# テスト経路。実トラフィックと同じ ALB の別ポート。Lambda(VPC外)から叩くので 0.0.0.0/0 で開けるが、
# 用途は使い捨て検証環境の green の事前検証のみ(アプリ側は devtoken 必須のまま)
resource "aws_security_group_rule" "alb_test" {
  type              = "ingress"
  security_group_id = aws_security_group.alb.id
  from_port         = 8081
  to_port           = 8081
  protocol          = "tcp"
  cidr_blocks       = ["0.0.0.0/0"]
}

resource "aws_lb_listener" "test" {
  load_balancer_arn = aws_lb.main.arn
  port              = 8081
  protocol          = "HTTP"
  default_action {
    type = "fixed-response"
    fixed_response {
      content_type = "text/plain"
      message_body = "no rule matched (test listener)"
      status_code  = "404"
    }
  }
}

resource "aws_lb_listener_rule" "photo_test" {
  listener_arn = aws_lb_listener.test.arn
  priority     = 10
  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.photo.arn
  }
  condition {
    path_pattern { values = ["/*"] }
  }
  lifecycle {
    # TEST_TRAFFIC_SHIFT 中は ECS がこのルールの forward 先を書き換える
    ignore_changes = [action]
  }
}

# --- 昇格ゲートのフック(Lambda) ------------------------------------------
# TEST_TRAFFIC_SHIFT 後に呼ばれ、テストリスナー経由で green を検証する。
# healthz だけでは PHOTO_FAULT(書き込みのみ壊れる)を素通しするので、**書き込みまで**やる。
variable "hook_devtoken" {
  description = "フックが書き込み検証に使う devtoken(dev/devtoken で発行)"
  sensitive   = true
  default     = ""
}

data "archive_file" "gate_hook" {
  type        = "zip"
  output_path = "${path.module}/.terraform/gate_hook.zip"
  source {
    filename = "index.py"
    content  = <<-PY
      import json, os, urllib.request, urllib.error

      def check():
          base = os.environ["TEST_URL"]
          # 1) 到達性
          for _ in range(3):
              with urllib.request.urlopen(base + "/healthz", timeout=5) as r:
                  if r.status != 200:
                      return False, f"healthz {r.status}"
          # 2) 書き込み(green の DB 経路まで通す)。healthz が緑でも書けない版をここで落とす
          req = urllib.request.Request(
              base + "/v2/photos",
              data=json.dumps({"caption": "gate-hook", "content_type": "image/png"}).encode(),
              headers={
                  "Content-Type": "application/json",
                  "Authorization": "Bearer " + os.environ["DEVTOKEN"],
              },
              method="POST",
          )
          try:
              with urllib.request.urlopen(req, timeout=10) as r:
                  if r.status != 201:
                      return False, f"create {r.status}"
          except urllib.error.HTTPError as e:
              return False, f"create {e.code}"
          return True, "ok"

      def handler(event, context):
          print("event:", json.dumps(event))
          try:
              ok, why = check()
          except Exception as e:  # 到達不能等
              ok, why = False, repr(e)
          print("verdict:", why)
          # ECS デプロイライフサイクルフックの契約: hookStatus を返す。
          # FAILED を返すとデプロイは失敗しロールバックする(本番トラフィックは動いていない)
          return {"hookStatus": "SUCCEEDED" if ok else "FAILED"}
    PY
  }
}

resource "aws_iam_role" "gate_hook_lambda" {
  name = "greenfield-71-gate-hook-lambda"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{ Effect = "Allow", Principal = { Service = "lambda.amazonaws.com" }, Action = "sts:AssumeRole" }]
  })
}

resource "aws_iam_role_policy_attachment" "gate_hook_logs" {
  role       = aws_iam_role.gate_hook_lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_lambda_function" "gate_hook" {
  function_name    = "greenfield-71-gate-hook"
  role             = aws_iam_role.gate_hook_lambda.arn
  runtime          = "python3.12"
  handler          = "index.handler"
  filename         = data.archive_file.gate_hook.output_path
  source_code_hash = data.archive_file.gate_hook.output_base64sha256
  timeout          = 30
  environment {
    variables = {
      TEST_URL = "http://${aws_lb.main.dns_name}:8081"
      DEVTOKEN = var.hook_devtoken
    }
  }
}

# ECS がフックを呼ぶときに assume するロール
resource "aws_iam_role" "hook_invoke" {
  name = "greenfield-71-hook-invoke"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{ Effect = "Allow", Principal = { Service = "ecs.amazonaws.com" }, Action = "sts:AssumeRole" }]
  })
  inline_policy {
    name = "invoke-gate-hook"
    policy = jsonencode({
      Version = "2012-10-17"
      Statement = [{ Effect = "Allow", Action = "lambda:InvokeFunction", Resource = aws_lambda_function.gate_hook.arn }]
    })
  }
}

# --- アラーム自動ロールバック用 -------------------------------------------
# サービス全体の 5xx。green/blue の TG は入れ替わるので、TG 単位でなく ALB 単位で見る
# (「ユーザーに 5xx が漏れているか」が判定基準そのもの)
resource "aws_cloudwatch_metric_alarm" "target_5xx" {
  alarm_name          = "greenfield-71-target-5xx"
  namespace           = "AWS/ApplicationELB"
  metric_name         = "HTTPCode_Target_5XX_Count"
  dimensions          = { LoadBalancer = aws_lb.main.arn_suffix }
  statistic           = "Sum"
  period              = 60
  evaluation_periods  = 1
  threshold           = 3
  comparison_operator = "GreaterThanOrEqualToThreshold"
  treat_missing_data  = "notBreaching"
}

output "test_url" { value = "http://${aws_lb.main.dns_name}:8081" }
output "gate_hook_arn" { value = aws_lambda_function.gate_hook.arn }
output "hook_invoke_role_arn" { value = aws_iam_role.hook_invoke.arn }
output "alarm_name" { value = aws_cloudwatch_metric_alarm.target_5xx.alarm_name }
output "test_listener_rule_arn" { value = aws_lb_listener_rule.photo_test.arn }

# 壊れた版の再現(G/A シナリオ)。通常は空
variable "photo_fault" { default = "" }
