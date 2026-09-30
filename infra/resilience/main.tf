# Phase 8 の回復性実測(#219 HA / #225 バックアップ・リストア)の使い捨てスタック。
#
# **使い捨て前提**: apply → 実測 → destroy を当日で回す。立てっぱなし禁止。
# state はローカル(このディレクトリ。gitignore 済み)。
#
# 設計判断:
#   - RDS **MySQL の Multi-AZ instance**(Aurora ではない)。測りたいのは古典的な
#     standby 昇格 + DNS フリップの窓で、7.1 と同じ engine 8.4 系で揃える
#   - backup_retention_period=1 で自動バックアップ + PITR を有効化(#225 の実測に必要)
#   - storage_encrypted は既定 true(#212)
#   - DB は公開しない(7.1/7.4 と同じ)。SQL・プローブは VPC 内 one-off ECS タスクで流し、
#     ログは CloudWatch から回収する。NAT なし、public subnet + SG
terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 6.0" }
  }
}

provider "aws" {
  region = var.region
  default_tags {
    tags = { Project = "greenfield", Stage = "8-resilience", Disposable = "true" }
  }
}

variable "region" { default = "ap-northeast-1" }
variable "db_password" { sensitive = true }

data "aws_availability_zones" "azs" { state = "available" }

resource "aws_vpc" "main" {
  cidr_block           = "10.81.0.0/16"
  enable_dns_hostnames = true
  tags                 = { Name = "greenfield-81" }
}

resource "aws_internet_gateway" "igw" { vpc_id = aws_vpc.main.id }

# networking 一式は 7.4 と同型。CIDR とタグだけ変える
resource "aws_subnet" "public" {
  count                   = 2
  vpc_id                  = aws_vpc.main.id
  cidr_block              = cidrsubnet(aws_vpc.main.cidr_block, 8, count.index)
  availability_zone       = data.aws_availability_zones.azs.names[count.index]
  map_public_ip_on_launch = true
  tags                    = { Name = "greenfield-81-public-${count.index}" }
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

resource "aws_security_group" "sqlrunner" {
  name   = "greenfield-81-sqlrunner"
  vpc_id = aws_vpc.main.id
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_security_group" "db" {
  name   = "greenfield-81-db"
  vpc_id = aws_vpc.main.id
  ingress {
    from_port       = 3306
    to_port         = 3306
    protocol        = "tcp"
    security_groups = [aws_security_group.sqlrunner.id]
  }
}

resource "aws_db_subnet_group" "main" {
  name       = "greenfield-81"
  subnet_ids = aws_subnet.public[*].id
}

resource "aws_db_instance" "mysql" {
  identifier              = "greenfield-81"
  engine                  = "mysql"
  engine_version          = "8.4.6"
  instance_class          = "db.t4g.micro"
  allocated_storage       = 20
  db_subnet_group_name    = aws_db_subnet_group.main.name
  vpc_security_group_ids  = [aws_security_group.db.id]
  username                = "root"
  password                = var.db_password
  multi_az                = true # #219 の主役
  backup_retention_period = 1    # #225: 自動バックアップ + PITR を有効化
  storage_encrypted       = true # 既定(#212)
  skip_final_snapshot     = true # 使い捨て。データに価値を持たせない
  apply_immediately       = true
}

# --- one-off SQL / プローブタスク(7.4 と同型) -------------------------------
resource "aws_ecs_cluster" "main" { name = "greenfield-81" }

resource "aws_cloudwatch_log_group" "sqlrunner" {
  name              = "/greenfield/81/sqlrunner"
  retention_in_days = 1
}

resource "aws_iam_role" "task_exec" {
  name = "greenfield-81-task-exec"
  assume_role_policy = jsonencode({
    Version   = "2012-10-17",
    Statement = [{ Effect = "Allow", Principal = { Service = "ecs-tasks.amazonaws.com" }, Action = "sts:AssumeRole" }]
  })
}

resource "aws_iam_role_policy_attachment" "task_exec" {
  role       = aws_iam_role.task_exec.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

resource "aws_ecs_task_definition" "sqlrunner" {
  family                   = "greenfield-81-sqlrunner"
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
    command   = ["sh", "-c", "echo sqlrunner-noop"]
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        awslogs-group         = aws_cloudwatch_log_group.sqlrunner.name
        awslogs-region        = var.region
        awslogs-stream-prefix = "sql"
      }
    }
  }])
}

output "db_endpoint" { value = aws_db_instance.mysql.endpoint }
output "db_id" { value = aws_db_instance.mysql.identifier }
output "subnets" { value = aws_subnet.public[*].id }
output "sqlrunner_sg" { value = aws_security_group.sqlrunner.id }
output "db_subnet_group" { value = aws_db_subnet_group.main.name }
output "db_sg" { value = aws_security_group.db.id }
