# Phase 7.5(#79)の使い捨てスタック。AWS AppConfig のフラグ基盤。
#
# **使い捨て前提**: apply → 検証 → destroy を当日で回す。state はローカル(gitignore 済み)。
# コンピュートも VPC も要らない(appconfigdata はパブリック API。アプリは手元で動かし、
# 実資格情報でポーリングする)。AppConfig の費用は構成リクエスト課金のみ。
#
# フラグ定義そのものはここに置かない。定義は git(deploy/compose/flagd/flags.json)が正で、
# フラグ専用パイプライン(dev/scripts/flags-deploy.sh)が AppConfig へ反映する。
# アプリのデプロイとフラグの反映を独立した2本のレバーに保つ(docs/adr/0011)。

terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 6.0" }
  }
}

provider "aws" {
  region = var.region
  default_tags {
    tags = { Project = "greenfield", Stage = "7.5", Disposable = "true" }
  }
}

variable "region" { default = "ap-northeast-1" }

resource "aws_appconfig_application" "main" {
  name        = "greenfield"
  description = "7.5 検証用。フラグ定義の反映先"
}

resource "aws_appconfig_environment" "prod" {
  name           = "prod"
  application_id = aws_appconfig_application.main.id
  # CloudWatch アラームによる自動ロールバック(monitor)は今回の検証範囲外。
  # 付ける場合はここに monitor ブロックでアラーム ARN を並べる
}

resource "aws_appconfig_configuration_profile" "flags" {
  name           = "feature-flags"
  application_id = aws_appconfig_application.main.id
  location_uri   = "hosted"
  type           = "AWS.AppConfig.FeatureFlags"
}

# 即時反映。キルスイッチ用(切替の所要を測る)
resource "aws_appconfig_deployment_strategy" "all_at_once" {
  name                           = "greenfield-all-at-once"
  deployment_duration_in_minutes = 0
  growth_factor                  = 100
  final_bake_time_in_minutes     = 0
  replicate_to                   = "NONE"
}

# 段階反映。2分かけて 50% ずつ + ベイク1分(検証用に短く。実運用は伸ばす)
resource "aws_appconfig_deployment_strategy" "gradual" {
  name                           = "greenfield-gradual"
  deployment_duration_in_minutes = 2
  growth_factor                  = 50
  growth_type                    = "LINEAR"
  final_bake_time_in_minutes     = 1
  replicate_to                   = "NONE"
}

output "application_id" { value = aws_appconfig_application.main.id }
output "environment_id" { value = aws_appconfig_environment.prod.environment_id }
output "profile_id" { value = aws_appconfig_configuration_profile.flags.configuration_profile_id }
output "strategy_all_at_once" { value = aws_appconfig_deployment_strategy.all_at_once.id }
output "strategy_gradual" { value = aws_appconfig_deployment_strategy.gradual.id }
