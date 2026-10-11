import type { HttpError } from "@refinedev/core";
import { useCustomMutation, useNotification } from "@refinedev/core";
import { useState } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { ListView, ListViewHeader } from "~/components/refine-ui/views/list-view";
import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "~/components/ui/alert-dialog";
import { Button } from "~/components/ui/button";
import { Input } from "~/components/ui/input";
import { Label } from "~/components/ui/label";
import { stepUpLogin } from "../providers";

export function meta() {
  return [{ title: "アカウント — greenfield admin" }];
}

// subject を入力して削除へ進む。削除は photos と同じ BFF(:delete)を叩き、AAL2 不足なら
// resume_subject を戻り先に載せて再認証へ(写真一覧の resume_delete と同じ型)。
export default function Accounts() {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const { open } = useNotification();
  const [subject, setSubject] = useState(params.get("resume_subject") ?? "");
  const [confirming, setConfirming] = useState(false);
  const [done, setDone] = useState<{ subject: string; n: number } | null>(null);
  const resumed = params.get("resume_subject");

  const { mutate: deleteAccount, mutation } = useCustomMutation();
  const runDelete = (s: string) =>
    deleteAccount(
      {
        url: `/resources/admin/accounts/${encodeURIComponent(s)}/delete`,
        method: "post",
        values: {},
        errorNotification: (err) => (err?.statusCode === 401 ? false : { type: "error", message: err?.message ?? "削除に失敗しました" }),
      },
      {
        onSuccess: (res) => {
          const n = (res.data as { deleted_photos?: number }).deleted_photos ?? 0;
          open?.({ type: "success", message: `${s} を削除しました（写真 ${n} 件）` });
          setDone({ subject: s, n });
          setSubject("");
        },
        onError: (err: HttpError & { code?: string }) => {
          if (err.statusCode === 401 && err.code === "auth.step_up_required") {
            navigate(stepUpLogin(`/accounts?${new URLSearchParams({ resume_subject: s })}`));
          }
        },
      },
    );

  return (
    <ListView>
      <ListViewHeader title="アカウント" canCreate={false} />
      {resumed && !done && (
        <Alert>
          <AlertTitle>再認証しました</AlertTitle>
          <AlertDescription>{resumed} の削除を続けられます。下のボタンから進めてください。</AlertDescription>
        </Alert>
      )}
      {done && (
        <Alert>
          <AlertTitle>削除しました</AlertTitle>
          <AlertDescription>
            {done.subject}（写真 {done.n} 件）
          </AlertDescription>
        </Alert>
      )}
      <form
        className="flex max-w-lg flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          if (subject.trim()) setConfirming(true);
        }}
      >
        <div className="space-y-1.5">
          <Label htmlFor="subject">削除する subject</Label>
          <Input id="subject" name="subject" value={subject} onChange={(e) => setSubject(e.target.value)} placeholder="例: alice" required />
        </div>
        <div>
          <Button type="submit" variant="destructive" disabled={!subject.trim() || mutation.isPending}>
            アカウントと全投稿を削除
          </Button>
        </div>
      </form>
      <AlertDialog open={confirming} onOpenChange={setConfirming}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{subject} のアカウントと全投稿を削除します</AlertDialogTitle>
            <AlertDialogDescription>取り消せません。AAL2（再認証）が必要です。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>やめる</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                setConfirming(false);
                runDelete(subject.trim());
              }}
            >
              削除する
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </ListView>
  );
}
