// Package @greenfield/photo-api は photo external API（メジャー2、/v2）の型定義。external.v2.gen.ts は `pnpm gen` で生成する。
import type { components, paths } from "./external.v2.gen.ts";

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
