// Package @greenfield/photo-api は photo external API の型定義。external.gen.ts は `pnpm gen` で生成する。
import type { components, paths } from "./external.gen.ts";

export type { components, paths };

export type Photo = components["schemas"]["Photo"];
export type CreatePhotoInput = components["schemas"]["CreatePhotoInputBody"];
export type CreatePhotoOutput = components["schemas"]["CreatePhotoOutputBody"];
export type Visibility = Photo["visibility"];
export type ImageContentType = CreatePhotoInput["content_type"];

export const imageContentTypes: readonly ImageContentType[] = [
  "image/jpeg",
  "image/png",
  "image/webp",
  "image/avif",
];
