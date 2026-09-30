# Phase 7 の最小構成(7.1 / #62)。単一 VPC + ALB + ECS Fargate + RDS MySQL。
#
# **使い捨て前提**: apply → 検証 → destroy を当日で回す。立てっぱなし禁止。
# state はローカル(このディレクトリ。gitignore 済み)。使い捨てなので remote state を持たない。
#
# コストの設計判断:
#   - NAT Gateway を置かない($0.062/h + 転送)。タスクはパブリックサブネットで public IP を持つ。
#     到達制御は SG で行う(external の ingress は ALB からのみ)
#   - RDS は db.t4g.micro / Single-AZ / 7.4 で Aurora を別に立てるのでここは素の MySQL
#   - ログは CloudWatch Logs(保持1日)

terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 6.0" }
    tls = { source = "hashicorp/tls", version = "~> 4.0" }
  }
}

provider "aws" {
  region = var.region
  default_tags {
    tags = { Project = "greenfield", Stage = "7.1", Disposable = "true" }
  }
}

variable "region" { default = "ap-northeast-1" }
variable "image" { description = "photo コンテナイメージ(ECR URI)" }
variable "deploy_marker" { default = "v1" } # taskdef の改版トリガー(B/G の再演用)
# ECS ネイティブの戦略。**BLUE_GREEN と CANARY は別物**(APIが排他を強制する。実測:
# BLUE_GREEN に canaryConfiguration を付けると InvalidParameterException。
# なお terraform provider はこの無効な組を黙って受理して canary を落とす)
variable "deploy_strategy" { default = "BLUE_GREEN" }

data "aws_availability_zones" "azs" { state = "available" }

# --- ネットワーク --------------------------------------------------------
resource "aws_vpc" "main" {
  cidr_block           = "10.70.0.0/16"
  enable_dns_hostnames = true
  tags                 = { Name = "greenfield-71" }
}

resource "aws_internet_gateway" "igw" { vpc_id = aws_vpc.main.id }

resource "aws_subnet" "public" {
  count                   = 2
  vpc_id                  = aws_vpc.main.id
  cidr_block              = cidrsubnet(aws_vpc.main.cidr_block, 8, count.index)
  availability_zone       = data.aws_availability_zones.azs.names[count.index]
  map_public_ip_on_launch = true
  tags                    = { Name = "greenfield-71-public-${count.index}" }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id
  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.igw.id
  }
}

resource "aws_route_table_association" "public" {
  count          = 2
  subnet_id      = aws_subnet.public[count.index].id
  route_table_id = aws_route_table.public.id
}

# --- セキュリティグループ --------------------------------------------------
resource "aws_security_group" "alb" {
  name   = "greenfield-71-alb"
  vpc_id = aws_vpc.main.id
  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  ingress {
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_security_group" "app" {
  name   = "greenfield-71-app"
  vpc_id = aws_vpc.main.id
  # external(8080) は ALB からのみ。internal/admin はこの構成では公開しない
  ingress {
    from_port       = 8080
    to_port         = 8080
    protocol        = "tcp"
    security_groups = [aws_security_group.alb.id]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_security_group" "db" {
  name   = "greenfield-71-db"
  vpc_id = aws_vpc.main.id
  ingress {
    from_port       = 3306
    to_port         = 3306
    protocol        = "tcp"
    security_groups = [aws_security_group.app.id]
  }
}

# --- RDS -----------------------------------------------------------------
resource "aws_db_subnet_group" "main" {
  name       = "greenfield-71"
  subnet_ids = aws_subnet.public[*].id
}

resource "aws_db_instance" "mysql" {
  identifier             = "greenfield-71"
  engine                 = "mysql"
  engine_version         = "8.4.6"
  instance_class         = "db.t4g.micro"
  allocated_storage      = 20
  db_subnet_group_name   = aws_db_subnet_group.main.name
  vpc_security_group_ids = [aws_security_group.db.id]
  username               = "root"
  password               = var.db_password
  skip_final_snapshot    = true # 使い捨て。データに価値を持たせない
  apply_immediately      = true
  # 公開しない。ブートストラップ(init SQL)も migrate も VPC 内の one-off ECS タスクで
  # 実行する(migrate は本番も ECS タスクで流す形なので、その形の予行にもなる)
  publicly_accessible = false
  # 保存時暗号化は既定(#212)。今どき無効にする理由がない
  storage_encrypted = true
}

variable "db_password" { sensitive = true }

# --- 秘密は SSM SecureString 経由で注入する(#210) -----------------------------
# taskdef の environment に平文で置くと、DescribeTaskDefinition・コンソール・
# tf plan の全部に露出する。ECS の secrets(valueFrom)なら実行時にだけ解決される。
resource "aws_ssm_parameter" "photo_dsn" {
  name  = "/greenfield/71/photo-dsn"
  type  = "SecureString"
  value = "photo_app:${var.db_password}@tcp(${aws_db_instance.mysql.address}:3306)/photo?parseTime=true"
}

resource "aws_ssm_parameter" "localauthz_dsn" {
  name  = "/greenfield/71/localauthz-dsn"
  type  = "SecureString"
  value = "localauthz:${var.db_password}@tcp(${aws_db_instance.mysql.address}:3306)/localauthz"
}

resource "aws_ssm_parameter" "photo_migrate_dsn" {
  name  = "/greenfield/71/photo-migrate-dsn"
  type  = "SecureString"
  value = "photo_migrate:${var.db_password}@tcp(${aws_db_instance.mysql.address}:3306)/photo?parseTime=true&multiStatements=true"
}

resource "aws_ssm_parameter" "db_root_password" {
  name  = "/greenfield/71/db-root-password"
  type  = "SecureString"
  value = var.db_password
}

data "aws_kms_alias" "ssm" { name = "alias/aws/ssm" }

resource "aws_iam_role_policy" "task_exec_ssm" {
  name = "ssm-secrets"
  role = aws_iam_role.task_exec.id
  policy = jsonencode({
    Version = "2012-10-17",
    Statement = [
      {
        Effect = "Allow",
        Action = ["ssm:GetParameters"],
        Resource = [
          aws_ssm_parameter.photo_dsn.arn,
          aws_ssm_parameter.localauthz_dsn.arn,
          aws_ssm_parameter.photo_migrate_dsn.arn,
          aws_ssm_parameter.db_root_password.arn,
        ]
      },
      { Effect = "Allow", Action = ["kms:Decrypt"], Resource = [data.aws_kms_alias.ssm.target_key_arn] }
    ]
  })
}

# --- ECR(先に -target で作ってイメージを push してから全体 apply する) ------
resource "aws_ecr_repository" "photo" {
  name         = "greenfield/photo"
  force_delete = true # 使い捨て
}

# --- ECS -----------------------------------------------------------------
resource "aws_ecs_cluster" "main" { name = "greenfield-71" }

resource "aws_cloudwatch_log_group" "photo" {
  name              = "/greenfield/71/photo"
  retention_in_days = 1
}

resource "aws_iam_role" "task_exec" {
  name = "greenfield-71-task-exec"
  assume_role_policy = jsonencode({
    Version   = "2012-10-17",
    Statement = [{ Effect = "Allow", Principal = { Service = "ecs-tasks.amazonaws.com" }, Action = "sts:AssumeRole" }]
  })
}

resource "aws_iam_role_policy_attachment" "task_exec" {
  role       = aws_iam_role.task_exec.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

# タスクロール: 実 S3(画像バケット)へのアクセス
resource "aws_iam_role" "task" {
  name               = "greenfield-71-task"
  assume_role_policy = aws_iam_role.task_exec.assume_role_policy
}

resource "aws_s3_bucket" "images" {
  bucket_prefix = "greenfield-71-images-"
  force_destroy = true # 使い捨て
}

resource "aws_iam_role_policy" "task_s3" {
  name = "images"
  role = aws_iam_role.task.id
  policy = jsonencode({
    Version = "2012-10-17",
    Statement = [{
      Effect   = "Allow",
      Action   = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:ListBucket"],
      Resource = [aws_s3_bucket.images.arn, "${aws_s3_bucket.images.arn}/*"]
    }]
  })
}

locals {
  photo_env = [
    { name = "ENV", value = "dev" }, # 7.1 は IaC の往復が主眼。認証は dev(staticauthn)。OIDC 差し替えは 7.2 以降
    { name = "PHOTO_EXTERNAL_ADDR", value = ":8080" },
    { name = "PHOTO_INTERNAL_ADDR", value = ":8081" },
    { name = "PHOTO_ADMIN_ADDR", value = ":8082" },
    { name = "METRICS_ADDR", value = ":9091" },
    { name = "PHOTO_IMAGE_BUCKET", value = aws_s3_bucket.images.bucket },
    { name = "AWS_REGION", value = var.region },
    { name = "FLAGS_SOURCE", value = "file" },
    { name = "FLAGS_FILE", value = "/etc/greenfield/flags.json" },
    { name = "DEPLOY_MARKER", value = var.deploy_marker },
    # 7.2b(#175): どの版が応答したかの観測口(ローカル予行 #172 と同じ)と、壊れた版の再現
    { name = "SERVICE_REVISION", value = var.deploy_marker },
    { name = "PHOTO_FAULT", value = var.photo_fault },
  ]
}

resource "aws_ecs_task_definition" "photo" {
  family                   = "greenfield-71-photo"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = 256
  memory                   = 512
  execution_role_arn       = aws_iam_role.task_exec.arn
  task_role_arn            = aws_iam_role.task.arn
  runtime_platform {
    cpu_architecture        = "ARM64" # t4g/Graviton 系で統一(ローカルも arm64 なのでビルドが素直)
    operating_system_family = "LINUX"
  }
  container_definitions = jsonencode([{
    name         = "photo"
    image        = var.image
    essential    = true
    portMappings = [{ containerPort = 8080 }]
    environment  = local.photo_env
    # 秘密は environment でなく secrets(実行時に SSM から解決。#210)
    secrets = [
      { name = "PHOTO_DSN", valueFrom = aws_ssm_parameter.photo_dsn.arn },
      { name = "LOCALAUTHZ_DSN", valueFrom = aws_ssm_parameter.localauthz_dsn.arn },
      { name = "PHOTO_MIGRATE_DSN", valueFrom = aws_ssm_parameter.photo_migrate_dsn.arn },
    ]
    # 書けるファイルシステムを持たない(#213)。ログは stdout、状態は DB/S3
    readonlyRootFilesystem = true
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        awslogs-group         = aws_cloudwatch_log_group.photo.name
        awslogs-region        = var.region
        awslogs-stream-prefix = "photo"
      }
    }
  }])
}

# DB 初期化(databases/users/GRANT)用の one-off タスク。SQL は run-task の
# command override で base64 で渡す(イメージを焼かない)。app SG を使うので db に届く
resource "aws_ecs_task_definition" "bootstrap" {
  family                   = "greenfield-71-bootstrap"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = 256
  memory                   = 512
  execution_role_arn       = aws_iam_role.task_exec.arn
  runtime_platform {
    cpu_architecture        = "ARM64"
    operating_system_family = "LINUX"
  }
  container_definitions = jsonencode([{
    name      = "mysql"
    image     = "public.ecr.aws/docker/library/mysql:8.4"
    essential = true
    command   = ["sh", "-c", "echo bootstrap-noop"]
    # mysql クライアントは MYSQL_PWD を自動で使う。run-task の command に -p を書くと
    # DescribeTasks の履歴に平文で残る(#210)ため、パスワードは secrets で注入する
    secrets = [{ name = "MYSQL_PWD", valueFrom = aws_ssm_parameter.db_root_password.arn }]
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        awslogs-group         = aws_cloudwatch_log_group.photo.name
        awslogs-region        = var.region
        awslogs-stream-prefix = "bootstrap"
      }
    }
  }])
}

resource "aws_lb" "main" {
  name               = "greenfield-71"
  load_balancer_type = "application"
  security_groups    = [aws_security_group.alb.id]
  subnets            = aws_subnet.public[*].id
}

resource "aws_lb_target_group" "photo" {
  name        = "greenfield-71-photo"
  port        = 8080
  protocol    = "HTTP"
  vpc_id      = aws_vpc.main.id
  target_type = "ip"
  health_check {
    path              = "/healthz"
    interval          = 10
    healthy_threshold = 2
  }
  # B/G(7.2)がターゲットグループを2枚使うのでここでは1枚に留める
}

# B/G はターゲットグループを2枚使い、ECS がリスナールールの向き先を掛け替える
resource "aws_lb_target_group" "photo_alt" {
  name        = "greenfield-71-photo-alt"
  port        = 8080
  protocol    = "HTTP"
  vpc_id      = aws_vpc.main.id
  target_type = "ip"
  health_check {
    path              = "/healthz"
    interval          = 10
    healthy_threshold = 2
  }
}

# 検証用の TLS(#211)。自己署名を ACM にインポートし、リスナー・リダイレクト・SG の
# 配線を実測する(ドメイン非依存。信頼チェーンは検証対象外なので確認は curl -k)。
# 本番は ACM 発行 + DNS 検証の証明書に置き換えるだけで、配線は同じ。
resource "tls_private_key" "alb" {
  algorithm   = "ECDSA"
  ecdsa_curve = "P256"
}

resource "tls_self_signed_cert" "alb" {
  private_key_pem       = tls_private_key.alb.private_key_pem
  validity_period_hours = 72
  allowed_uses          = ["key_encipherment", "digital_signature", "server_auth"]
  subject {
    common_name = "greenfield-71.invalid"
  }
}

resource "aws_acm_certificate" "alb" {
  private_key      = tls_private_key.alb.private_key_pem
  certificate_body = tls_self_signed_cert.alb.cert_pem
}

resource "aws_lb_listener" "https" {
  load_balancer_arn = aws_lb.main.arn
  port              = 443
  protocol          = "HTTPS"
  ssl_policy        = "ELBSecurityPolicy-TLS13-1-2-2021-06"
  certificate_arn   = aws_acm_certificate.alb.arn
  # 既定は 404 の固定応答。実トラフィックは下の**ルール**が運ぶ
  # (ECS ネイティブ B/G は「リスナールール」を操作するため、default_action では管理できない)
  default_action {
    type = "fixed-response"
    fixed_response {
      content_type = "text/plain"
      message_body = "no rule matched"
      status_code  = "404"
    }
  }
}

# 80 は受けるが運ばない。Bearer トークンが平文 HTTP を流れる経路を残さない(#211)
resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.main.arn
  port              = 80
  protocol          = "HTTP"
  default_action {
    type = "redirect"
    redirect {
      port        = "443"
      protocol    = "HTTPS"
      status_code = "HTTP_301"
    }
  }
}

resource "aws_lb_listener_rule" "photo" {
  listener_arn = aws_lb_listener.https.arn
  priority     = 10
  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.photo.arn
  }
  condition {
    path_pattern { values = ["/*"] }
  }
  lifecycle {
    # B/G 中は ECS がこのルールの forward 先(重み)を書き換える。tf が戻さないように
    ignore_changes = [action]
  }
}

# ECS が ELB を操作するためのインフラロール(ネイティブ B/G の要件)
resource "aws_iam_role" "ecs_infra" {
  name = "greenfield-71-ecs-infra"
  assume_role_policy = jsonencode({
    Version   = "2012-10-17",
    Statement = [{ Effect = "Allow", Principal = { Service = "ecs.amazonaws.com" }, Action = "sts:AssumeRole" }]
  })
}

resource "aws_iam_role_policy_attachment" "ecs_infra" {
  role       = aws_iam_role.ecs_infra.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonECSInfrastructureRolePolicyForLoadBalancers"
}

resource "aws_ecs_service" "photo" {
  name            = "photo"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.photo.arn
  desired_count   = 1
  launch_type     = "FARGATE"
  network_configuration {
    subnets          = aws_subnet.public[*].id
    security_groups  = [aws_security_group.app.id]
    assign_public_ip = true # NAT を置かない代わり。到達制御は SG(8080 は ALB からのみ)
  }
  load_balancer {
    target_group_arn = aws_lb_target_group.photo.arn
    container_name   = "photo"
    container_port   = 8080
    advanced_configuration {
      alternate_target_group_arn = aws_lb_target_group.photo_alt.arn
      production_listener_rule   = aws_lb_listener_rule.photo.arn
      role_arn                   = aws_iam_role.ecs_infra.arn
    }
  }
  deployment_configuration {
    # ECS ネイティブ(CodeDeploy 不要。docs/conventions/internal-06 §10.3)。
    strategy             = var.deploy_strategy
    bake_time_in_minutes = 2
    dynamic "canary_configuration" {
      for_each = var.deploy_strategy == "CANARY" ? [1] : []
      content {
        canary_percent              = 20
        canary_bake_time_in_minutes = 1
      }
    }
  }
  depends_on = [aws_lb_listener_rule.photo, aws_iam_role_policy_attachment.ecs_infra]
}

output "alb_dns" { value = aws_lb.main.dns_name }
output "db_address" { value = aws_db_instance.mysql.address }
output "images_bucket" { value = aws_s3_bucket.images.bucket }
