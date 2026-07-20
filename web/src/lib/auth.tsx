import { type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "@/lib/api";
import { AuthContext } from "@/lib/auth-context";

export function AuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();

  const setupQuery = useQuery({
    queryKey: ["setup-status"],
    queryFn: api.setupStatus,
  });

  const sessionQuery = useQuery({
    queryKey: ["session"],
    queryFn: api.session,
    retry: false,
    enabled: setupQuery.data?.setupComplete === true,
    // A 401 here just means "not logged in" — not an error state worth
    // surfacing, so treat it as "no user" rather than a query error.
    throwOnError: (error) => !(error instanceof ApiError && error.status === 401),
  });

  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey: ["setup-status"] });
    await queryClient.invalidateQueries({ queryKey: ["session"] });
  };

  return (
    <AuthContext.Provider
      value={{
        user: sessionQuery.data,
        isLoading: setupQuery.isLoading || sessionQuery.isLoading,
        setupComplete: setupQuery.data?.setupComplete,
        refresh,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}
