import { createContext } from "react-router";
import type { GearClient } from "./.server/gear";
import type { PhotoClient } from "./.server/photo";

export type Viewer = { subject: string; aal: number };

export const viewerContext = createContext<Viewer | null>(null);
export const photoClientContext = createContext<PhotoClient>();
export const gearClientContext = createContext<GearClient>();
export const requestIdContext = createContext<string>();
export const traceparentContext = createContext<string>();
