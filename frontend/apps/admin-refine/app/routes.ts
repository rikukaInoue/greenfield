import { type RouteConfig, index, layout, route } from "@react-router/dev/routes";

export default [
  route("login", "routes/login.tsx"),
  route("logout", "routes/logout.ts"),
  layout("routes/authed.tsx", [
    index("routes/photos.tsx"),
    // BFF: refine の dataProvider はブラウザで動くので、admin API を直接叩かせずここを叩かせる。
    // サーバ側がセッションのトークンで代理呼び出しする（トークンはブラウザに出ない）
    route("resources/admin/photos", "routes/resources.admin.photos.ts"),
    route("resources/admin/accounts/:subject/delete", "routes/resources.admin.accounts.delete.ts"),
  ]),
] satisfies RouteConfig;
