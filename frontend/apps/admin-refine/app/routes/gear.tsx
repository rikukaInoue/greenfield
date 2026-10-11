import { ListView, ListViewHeader } from "~/components/refine-ui/views/list-view";
import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert";

export function meta() {
  return [{ title: "機材 — greenfield admin" }];
}

// gear の admin API は未実装(api/gear/admin.openapi.json に paths が無い)。
// メニュー項目が増えるときの形だけ見せる。API ができたら photos と同じ BFF + DataTable で埋める
export default function Gear() {
  return (
    <ListView>
      <ListViewHeader title="機材" canCreate={false} />
      <Alert>
        <AlertTitle>準備中</AlertTitle>
        <AlertDescription>gear の管理 API が未実装です。契約(admin.openapi.json)ができたら一覧を載せます。</AlertDescription>
      </Alert>
    </ListView>
  );
}
