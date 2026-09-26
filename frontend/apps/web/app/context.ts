import { createContext } from "react-router";
import type { FlagValues } from "./.server/flags";
import type { PhotoClient } from "./.server/photo";

export type Viewer = { subject: string; aal: number };

export const viewerContext = createContext<Viewer | null>(null);
export const photoClientContext = createContext<PhotoClient>();
export const requestIdContext = createContext<string>();
export const flagsContext = createContext<FlagValues>();
