import { redirect } from "react-router";
import type { Route } from "./+types/logout";
import { sessionStorage } from "../.server/session";

export async function action({ request }: Route.ActionArgs) {
  const session = await sessionStorage.getSession(request.headers.get("Cookie"));
  return redirect("/login", { headers: { "Set-Cookie": await sessionStorage.destroySession(session) } });
}

export function loader() {
  return redirect("/");
}
