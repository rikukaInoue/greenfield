import type { HttpError } from "@refinedev/core";
import { useCustomMutation, useInvalidate, useNotification } from "@refinedev/core";
import { useTable } from "@refinedev/react-table";
import type { AdminPhoto } from "@greenfield/photo-api/admin";
import type { ColumnDef } from "@tanstack/react-table";
import { useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router";
import type { Route } from "./+types/photos";
import { DataTable } from "~/components/refine-ui/data-table/data-table";
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
import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import { Input } from "~/components/ui/input";
import { stepUpLogin } from "../providers";

export function meta() {
  return [{ title: "全ユーザーの写真 — greenfield admin" }];
}

// 一覧と削除。データ取得は refine のフック（ブラウザ側）→ BFF（/resources/admin/*）→ admin API。
// サーバ側 loader でデータを取っていないので、SSR の初期 HTML は骨格だけになる（比較の論点）。
export default function Photos(_: Route.ComponentProps) {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const { open } = useNotification();
  const invalidate = useInvalidate();
  // 再認証から戻ってきたら、進行中だった削除を続けられるように対象を URL で持ち回る
  const [pendingDelete, setPendingDelete] = useState<string | null>(params.get("resume_delete"));
  const [confirming, setConfirming] = useState<string | null>(null);

  const { mutate: deleteAccount, mutation } = useCustomMutation();
  const runDelete = (subject: string) =>
    deleteAccount(
      {
        url: `/resources/admin/accounts/${encodeURIComponent(subject)}/delete`,
        method: "post",
        values: {},
        // 401(ステップアップ)は遷移で扱うので、refine 既定のエラートーストを出させない
        // （出すと再認証後の画面にまで "Error (status code: 401)" が残る。実測で確認）
        errorNotification: (err) =>
          err?.statusCode === 401 ? false : { type: "error", message: err?.message ?? "削除に失敗しました" },
      },
      {
        onSuccess: (res) => {
          const n = (res.data as { deleted_photos?: number }).deleted_photos ?? 0;
          open?.({ type: "success", message: `${subject} を削除しました（写真 ${n} 件）` });
          setPendingDelete(null);
          invalidate({ resource: "photos", invalidates: ["list"] });
        },
        onError: (err: HttpError & { code?: string }) => {
          // AAL2 が足りない: 進行中の操作を戻り先に載せて再認証へ
          if (err.statusCode === 401 && err.code === "auth.step_up_required") {
            navigate(stepUpLogin(`/?${new URLSearchParams({ resume_delete: subject })}`));
          }
        },
      },
    );

  const columns = useMemo<ColumnDef<AdminPhoto>[]>(
    () => [
      { id: "id", accessorKey: "id", header: "ID", size: 70 },
      { id: "owner_id", accessorKey: "owner_id", header: "投稿者" },
      { id: "caption", accessorKey: "caption", header: "キャプション" },
      {
        id: "visibility",
        accessorKey: "visibility",
        header: "公開",
        cell: ({ getValue }) => <Badge variant={getValue() === "public" ? "default" : "secondary"}>{String(getValue())}</Badge>,
      },
      {
        id: "status",
        accessorKey: "status",
        header: "状態",
        cell: ({ getValue }) => <Badge variant={getValue() === "ready" ? "default" : "outline"}>{String(getValue())}</Badge>,
      },
      { id: "created_at", accessorKey: "created_at", header: "投稿時刻" },
      {
        id: "actions",
        header: "操作",
        cell: ({ row }) => (
          <Button variant="destructive" size="sm" aria-label={`${row.original.owner_id} のアカウントを削除`} onClick={() => setConfirming(row.original.owner_id)}>
            アカウント削除
          </Button>
        ),
      },
    ],
    [],
  );

  const table = useTable<AdminPhoto, HttpError>({
    columns,
    refineCoreProps: {
      resource: "photos",
      syncWithLocation: false,
      // admin API はページング無し(limit のみ)。サーバ側ページングは切り、取れた分を
      // TanStack 側でページ送りする。既定の 10 行/頁のままだと 50 件取っても 10 件しか
      // 見えない（実測で気づいた refine-ui DataTable の既定値）
      pagination: { mode: "off" },
    },
    initialState: { pagination: { pageSize: 50 } },
  });
  const { setFilters, filters } = table.refineCore;
  const ownerFilter = filters.find((f) => "field" in f && f.field === "owner_id");
  const ownerValue = ownerFilter && "value" in ownerFilter ? String(ownerFilter.value ?? "") : "";

  return (
    <ListView>
      <ListViewHeader title="全ユーザーの写真" canCreate={false} />
      {pendingDelete && (
        <Alert>
          <AlertTitle>再認証しました</AlertTitle>
          <AlertDescription className="flex items-center justify-between gap-4">
            <span>{pendingDelete} の削除を続けますか？</span>
            <span className="flex gap-2">
              <Button size="sm" variant="destructive" disabled={mutation.isPending} onClick={() => runDelete(pendingDelete)}>
                削除を続ける
              </Button>
              <Button size="sm" variant="outline" onClick={() => setPendingDelete(null)}>
                やめる
              </Button>
            </span>
          </AlertDescription>
        </Alert>
      )}
      <form
        className="flex gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          const v = String(new FormData(e.currentTarget).get("owner_id") ?? "").trim();
          setFilters([{ field: "owner_id", operator: "eq", value: v || undefined }], "replace");
        }}
      >
        <Input name="owner_id" aria-label="owner_id で絞る" placeholder="owner_id で絞る" defaultValue={ownerValue} className="w-72" />
        <Button type="submit" variant="secondary">
          絞り込む
        </Button>
      </form>
      <DataTable table={table} />
      <AlertDialog open={confirming !== null} onOpenChange={(o) => !o && setConfirming(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{confirming} のアカウントと全投稿を削除します</AlertDialogTitle>
            <AlertDialogDescription>取り消せません。AAL2（再認証）が必要です。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>やめる</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (confirming) runDelete(confirming);
                setConfirming(null);
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
