# ゲート判定ロジックの回帰テスト(#226)。ゲートが fail open する変異
# (「500 でも SUCCEEDED」等)をここで止める。実行: mise run audit:checks
import io
import os
import unittest
import urllib.error

os.environ["TEST_URL"] = "http://test.invalid"
os.environ["DEVTOKEN"] = "test-token"
import index  # noqa: E402


class FakeResponse(io.BytesIO):
    def __init__(self, status):
        super().__init__(b"{}")
        self.status = status

    def __enter__(self):
        return self

    def __exit__(self, *a):
        return False


def opener(statuses):
    calls = iter(statuses)

    def urlopen(req, timeout=None):
        s = next(calls)
        if isinstance(s, Exception):
            raise s
        return FakeResponse(s)

    return urlopen


class TestGate(unittest.TestCase):
    def test_healthy_and_writable_succeeds(self):
        ok, why = index.check(opener([200, 200, 200, 201]))
        self.assertTrue(ok)
        self.assertEqual(why, "ok")

    def test_healthz_green_but_write_broken_fails(self):
        # これがこのゲートの存在理由: healthz 緑・書き込みだけ 500 を落とす
        ok, why = index.check(opener([200, 200, 200,
                                      urllib.error.HTTPError("u", 500, "boom", {}, io.BytesIO())]))
        self.assertFalse(ok)
        self.assertEqual(why, "create 500")

    def test_unhealthy_fails(self):
        ok, why = index.check(opener([503]))
        self.assertFalse(ok)
        self.assertEqual(why, "healthz 503")

    def test_write_not_created_fails(self):
        # 200(既存を返した等)も「作れていない」なので落とす。201 だけが成功
        ok, why = index.check(opener([200, 200, 200, 200]))
        self.assertFalse(ok)
        self.assertEqual(why, "create 200")


if __name__ == "__main__":
    unittest.main()
