// Package @greenfield/gear-api は gear external API（メジャー1）の型定義。external.gen.ts は `pnpm gen` で生成する。
import type { components, paths } from "./external.gen.ts";

export type { components, paths };

export type GearItem = components["schemas"]["Item"];
export type ListedGearItem = components["schemas"]["ListedItem"];
