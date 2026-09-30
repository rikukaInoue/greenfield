# 昇格ゲートのフック(ECS デプロイライフサイクル)。TEST_TRAFFIC_SHIFT 後に呼ばれ、
# テストリスナー経由で green を検証する。healthz だけでは「書き込みだけ壊れた版」を
# 素通しするので、業務の書き込みまでやる(#172 / 10.3)。
#
# **このファイル自体の回帰テスト**が index_test.py にある(#226)。ゲートは壊れると
# 「全デプロイを止める」か「壊れた版を通す」の両極に倒れるため、判定ロジックは
# インライン heredoc でなくファイルとして持ち、単体テストで固定する。
import json
import os
import urllib.error
import urllib.request

try:
    import boto3  # Lambda ランタイムには入っている。テスト環境には無くてよい
except ImportError:  # pragma: no cover
    boto3 = None

_token_cache = None


def token():
    # devtoken は Lambda の環境変数に平文で置かず、実行時に SSM から解決する(#210)。
    # テストは DEVTOKEN を直接注入する(SSM もネットワークも不要にするため)
    global _token_cache
    if os.environ.get("DEVTOKEN"):
        return os.environ["DEVTOKEN"]
    if _token_cache is None:
        _token_cache = boto3.client("ssm").get_parameter(
            Name=os.environ["DEVTOKEN_PARAM"], WithDecryption=True
        )["Parameter"]["Value"]
    return _token_cache


def check(urlopen=urllib.request.urlopen):
    base = os.environ["TEST_URL"]
    # 1) 到達性
    for _ in range(3):
        with urlopen(base + "/healthz", timeout=5) as r:
            if r.status != 200:
                return False, f"healthz {r.status}"
    # 2) 書き込み(green の DB 経路まで通す)。healthz が緑でも書けない版をここで落とす。
    #    このテスト書き込みは commit しない = pending_upload のまま回収ジョブの対象(#199)
    req = urllib.request.Request(
        base + "/v2/photos",
        data=json.dumps({"caption": "gate-hook", "content_type": "image/png"}).encode(),
        headers={
            "Content-Type": "application/json",
            "Authorization": "Bearer " + token(),
        },
        method="POST",
    )
    try:
        with urlopen(req, timeout=10) as r:
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
    # FAILED を返すとデプロイは失敗しロールバックする(本番トラフィックは動いていない)
    return {"hookStatus": "SUCCEEDED" if ok else "FAILED"}
