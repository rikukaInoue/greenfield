import { Form } from "react-router";
import { LogOutIcon } from "lucide-react";
import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import { SidebarTrigger, useSidebar } from "~/components/ui/sidebar";
import type { Viewer } from "../context";

// レジストリの Header(layout-01)は useGetIdentity + useLogout(=authProvider 経由の SPA ログアウト)
// 前提で、テーマ切替も同梱している。こちらは要件どおり「operator 名 / AAL / ログアウト」だけを
// サーバ側セッション(loader の viewer)から描き、ログアウトは POST /logout(状態を変えるのは POST だけ)。
export function AdminHeader({ viewer }: { viewer: Viewer | null }) {
  const { isMobile } = useSidebar();
  return (
    // SidebarInset は <main> を描くので、その中の <header> は暗黙の banner ロールを失う
    // (ARIA の規則)。e2e と支援技術のために明示する
    <header role="banner" className="bg-sidebar sticky top-0 z-40 flex h-14 shrink-0 items-center justify-between border-b px-3">
      <div className="flex items-center gap-2">{isMobile && <SidebarTrigger />}</div>
      {viewer && (
        <div className="flex items-center gap-3 text-sm">
          <span>{viewer.subject}</span>
          <Badge variant={viewer.aal >= 2 ? "default" : "secondary"}>AAL{viewer.aal}</Badge>
          <Form method="post" action="/logout">
            <Button type="submit" variant="ghost" size="sm">
              <LogOutIcon />
              ログアウト
            </Button>
          </Form>
        </div>
      )}
    </header>
  );
}
