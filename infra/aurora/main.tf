# Phase 7.4(#65)の使い捨てスタック。Aurora MySQL での INSTANT DDL 挙動と
# fast clone(copy-on-write restore)を実測する。
#
# **使い捨て前提**: apply → 検証 → destroy を当日で回す。立てっぱなし禁止。
# state はローカル(このディレクトリ。gitignore 済み)。
#
# 設計判断:
#   - エンジンは **8.4.mysql_aurora.8.4.8**(MySQL 8.4 互換)。ローカルの実測が 8.4.11 なので、
#     8.0 系を選ぶと「Aurora の差」と「8.0/8.4 の差」が交絡する。8.4 互換で揃えて Aurora だけを測る
#   - DB は公開しない(7.1 と同じ)。SQL は VPC 内の one-off ECS タスク(mysql:8.4)で流す
#   - NAT を置かない。タスクはパブリックサブネット + public IP、到達制御は SG
#   - clone は Terraform で持たない(restore-db-cluster-to-point-in-time は CLI で実測し、
#     計測後すぐ消す。tf state に入れると destroy の順序管理が増えるだけ)

terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 6.0" }
  }
}

provider "aws" {
  region = var.region
  default_tags {
    tags = { Project = "greenfield", Stage = "7.4", Disposable = "true" }
  }
}

variable "region" { default = "ap-northeast-1" }
variable "db_password" { sensitive = true }

data "aws_availability_zones" "azs" { state = "available" }

# --- ネットワーク --------------------------------------------------------
resource "aws_vpc" "main" {
  cidr_block           = "10.71.0.0/16"
  enable_dns_hostnames = true
  tags                 = { Name = "greenfield-74" }
}

resource "aws_internet_gateway" "igw" { vpc_id = aws_vpc.main.id }

resource "aws_subnet" "public" {
  count                   = 2
  vpc_id                  = aws_vpc.main.id
  cidr_block              = cidrsubnet(aws_vpc.main.cidr_block, 8, count.index)
  availability_zone       = data.aws_availability_zones.azs.names[count.index]
  map_public_ip_on_launch = true
  tags                    = { Name = "greenfield-74-public-${count.index}" }
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
resource "aws_security_group" "sqlrunner" {
  name   = "greenfield-74-sqlrunner"
  vpc_id = aws_vpc.main.id
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_security_group" "db" {
  name   = "greenfield-74-db"
  vpc_id = aws_vpc.main.id
  ingress {
    from_port       = 3306
    to_port         = 3306
    protocol        = "tcp"
    security_groups = [aws_security_group.sqlrunner.id]
  }
}

# --- Aurora ----------------------------------------------------------------
resource "aws_db_subnet_group" "main" {
  name       = "greenfield-74"
  subnet_ids = aws_subnet.public[*].id
}

resource "aws_rds_cluster" "aurora" {
  cluster_identifier     = "greenfield-74"
  engine                 = "aurora-mysql"
  engine_version         = "8.4.mysql_aurora.8.4.8"
  master_username        = "root"
  master_password        = var.db_password
  db_subnet_group_name   = aws_db_subnet_group.main.name
  vpc_security_group_ids = [aws_security_group.db.id]
  skip_final_snapshot    = true # 使い捨て。データに価値を持たせない
  apply_immediately      = true
}

resource "aws_rds_cluster_instance" "writer" {
  identifier          = "greenfield-74-writer"
  cluster_identifier  = aws_rds_cluster.aurora.id
  engine              = aws_rds_cluster.aurora.engine
  engine_version      = aws_rds_cluster.aurora.engine_version
  instance_class      = "db.t4g.medium" # Aurora の最小クラス帯
  publicly_accessible = false
}

# --- one-off SQL タスク(7.1 の bootstrap と同じ形) --------------------------
resource "aws_ecs_cluster" "main" { name = "greenfield-74" }

resource "aws_cloudwatch_log_group" "sqlrunner" {
  name              = "/greenfield/74/sqlrunner"
  retention_in_days = 1
}

resource "aws_iam_role" "task_exec" {
  name = "greenfield-74-task-exec"
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
  family                   = "greenfield-74-sqlrunner"
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

# --- 出力(計測スクリプトが使う) ---------------------------------------------
output "cluster_endpoint" { value = aws_rds_cluster.aurora.endpoint }
output "cluster_id" { value = aws_rds_cluster.aurora.cluster_identifier }
output "subnets" { value = aws_subnet.public[*].id }
output "sqlrunner_sg" { value = aws_security_group.sqlrunner.id }
output "db_subnet_group" { value = aws_db_subnet_group.main.name }
output "db_sg" { value = aws_security_group.db.id }
