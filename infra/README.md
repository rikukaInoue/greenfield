# infra/ — Phase 7 の使い捨て AWS 構成

**apply → 検証 → destroy を当日で回す。立てっぱなし禁止**(05-roadmap Phase 7)。

```sh
terraform init
# 1. ECR だけ先に作ってイメージを push
terraform apply -target=aws_ecr_repository.photo -var image=dummy -var db_password=dummy
docker build -f ../services/photo/Dockerfile -t <ECR URI>:<tag> .. && docker push <ECR URI>:<tag>
# 2. 全体 apply(RDS 起動 ~8分)
terraform apply -var image=<ECR URI>:<tag> -var db_password=<使い捨ての値> -var admin_cidr=<自分のIP>/32
# 3. DB 初期化 + migrate(手元から。bootstrap の手順は verification-log 参照)
# 4. 検証 → destroy
terraform destroy -var image=... -var db_password=...
```

- state はローカル(gitignore 済み)。使い捨てなので remote state を持たない
- NAT Gateway は置かない。タスクはパブリックサブネット + SG で到達制御
- 秘密は tfvars に置かず -var で渡す(履歴に残さない)
