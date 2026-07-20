import { createContext, useContext } from "react";
import type { User } from "@/lib/api";

export interface AuthContextValue {
  user: User | undefined;
  isLoading: boolean;
  setupComplete: boolean | undefined;
  refresh: () => Promise<void>;
}

export const AuthContext = createContext<AuthContextValue | undefined>(undefined);

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) {
    throw new Error("useAuth must be used within an AuthProvider");
  }
  return ctx;
}
