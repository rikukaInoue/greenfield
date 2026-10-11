// @greenfield/photo-api/admin は photo admin API（社内オペレータ）の型定義。admin.gen.ts は `pnpm gen` で生成する。
// external とは別の面（リスナーも契約も別）なので、型も混ぜない。
import type { components, paths } from "./admin.gen.ts";

export type { components, paths };

export type AdminPhoto = components["schemas"]["AdminPhoto"];
export type AdminDeleteAccountOutput = components["schemas"]["AdminDeleteAccountOutputBody"];
