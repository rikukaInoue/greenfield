import { createContext } from "react-router";
import type { AdminClient } from "./.server/admin";

export type Viewer = { subject: string; aal: number };

export const viewerContext = createContext<Viewer | null>(null);
export const adminClientContext = createContext<AdminClient>();
export const requestIdContext = createContext<string>();
