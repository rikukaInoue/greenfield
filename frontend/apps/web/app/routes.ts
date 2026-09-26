import { type RouteConfig, index, layout, route } from "@react-router/dev/routes";

export default [
  route("login", "routes/login.tsx"),
  route("logout", "routes/logout.tsx"),
  layout("routes/authed.tsx", [
    index("routes/photos.tsx"),
    route("photos/new", "routes/photos.new.tsx"),
    route("photos/:id", "routes/photos.detail.tsx"),
    route("resources/photos", "routes/resources.photos.ts"),
    route("resources/photos/:id/commit", "routes/resources.photos.commit.ts"),
  ]),
] satisfies RouteConfig;
