import type { Photo } from "@greenfield/photo-api";

export function VisibilityBadge({ visibility }: { visibility: Photo["visibility"] }) {
  const cls =
    visibility === "public"
      ? "bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300"
      : "bg-stone-200 text-stone-700 dark:bg-stone-800 dark:text-stone-300";
  return <span className={`rounded px-1.5 py-0.5 text-xs ${cls}`}>{visibility === "public" ? "公開" : "非公開"}</span>;
}

export function PhotoImage({ photo, className }: { photo: Photo; className?: string }) {
  if (!photo.image_url) {
    return <div className={`flex items-center justify-center bg-stone-200 text-sm text-stone-500 dark:bg-stone-800 ${className}`}>画像なし</div>;
  }
  return <img src={photo.image_url} alt={photo.caption} className={`object-cover ${className}`} />;
}

export function formatDate(iso: string) {
  return new Date(iso).toLocaleString("ja-JP", { dateStyle: "medium", timeStyle: "short", timeZone: "Asia/Tokyo" });
}
