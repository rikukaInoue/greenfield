# infra/aurora — 7.4(#65)の使い捨て Aurora スタック

Aurora MySQL(8.4 互換)での INSTANT DDL 判定と fast clone を実測するための構成。
**apply → 計測 → destroy を当日で回す。立てっぱなし禁止。** state はローカル(gitignore 済み)。

実測結果は [docs/verification-log/2026-09-30-stage-74.md](../../docs/verification-log/2026-09-30-stage-74.md)。

## 手順

```sh
terraform init
terraform apply -var db_password=<password>   # クラスタ+インスタンスで 10 分前後

# DDL 計測(one-off ECS タスクで ddl-measure.sh を流す。DB は非公開のまま)
# ローカル比較: DBHOST=127.0.0.1 DBPORT=13306 DBPW=root sh ddl-measure.sh

# fast clone(Terraform で持たない。計測後すぐ消すため CLI):
aws rds restore-db-cluster-to-point-in-time \
  --source-db-cluster-identifier greenfield-74 \
  --db-cluster-identifier greenfield-74-clone \
  --restore-type copy-on-write --use-latest-restorable-time \
  --db-subnet-group-name <output: db_subnet_group> \
  --vpc-security-group-ids <output: db_sg>
aws rds create-db-instance --db-instance-identifier greenfield-74-clone-writer \
  --db-cluster-identifier greenfield-74-clone --engine aurora-mysql \
  --db-instance-class db.t4g.medium

# 片付け(clone は instance → cluster の順。その後に destroy)
aws rds delete-db-instance --db-instance-identifier greenfield-74-clone-writer --skip-final-snapshot
aws rds delete-db-cluster --db-cluster-identifier greenfield-74-clone --skip-final-snapshot
terraform destroy -var db_password=<password>
```
