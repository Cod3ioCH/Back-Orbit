import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { Navigate, useNavigate } from "react-router-dom";
import { useMutation } from "@tanstack/react-query";
import { OrbitIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { api, ApiError } from "@/lib/api";
import { useAuth } from "@/lib/auth-context";
import { FullPageSpinner } from "@/components/layout/FullPageSpinner";

const schema = z.object({
  username: z.string().min(1, "Required"),
  password: z.string().min(1, "Required"),
});

type FormValues = z.infer<typeof schema>;

export function LoginPage() {
  const navigate = useNavigate();
  const { refresh, user, setupComplete, isLoading } = useAuth();
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<FormValues>({ resolver: zodResolver(schema) });

  const mutation = useMutation({
    mutationFn: (values: FormValues) => api.login(values.username, values.password),
    onSuccess: async () => {
      await refresh();
      navigate("/", { replace: true });
    },
  });

  // Wait for the setup/session queries before deciding anything. Rendering the
  // form first would flash a sign-in prompt at someone who is about to be sent
  // to initial setup instead.
  if (isLoading) {
    return <FullPageSpinner />;
  }

  // Redirect via <Navigate> rather than calling navigate() here: this runs
  // during render, and triggering a router state update from inside render
  // is a side effect React warns about.
  //
  // With no administrator account yet there is nothing to sign in to, and the
  // form asks for a password that cannot exist. This is reachable on a fresh
  // install because the URL outlives the data: logging out navigates here, and
  // browsers restore the tab and offer it from history long after the database
  // behind it was wiped. It mirrors SetupPage's guard in the other direction.
  if (setupComplete === false) {
    return <Navigate to="/setup" replace />;
  }
  if (user) {
    return <Navigate to="/" replace />;
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-muted/30 p-4">
      <Card className="w-full max-w-sm">
        {/* The icon needs its own centering wrapper: CardHeader lays its
            children out in a grid, so `items-center` alone left-aligned the
            icon while the text was centred. */}
        <CardHeader className="text-center">
          <div className="flex justify-center">
            <OrbitIcon className="mb-2 size-8 text-primary" aria-hidden="true" />
          </div>
          <CardTitle>Sign in to Back-Orbit</CardTitle>
          <CardDescription>Enter your administrator credentials.</CardDescription>
        </CardHeader>
        <CardContent>
          <form
            className="space-y-4"
            onSubmit={handleSubmit((values) => mutation.mutate(values))}
            noValidate
          >
            <div className="space-y-2">
              <Label htmlFor="username">Username</Label>
              <Input id="username" autoComplete="username" {...register("username")} />
              {errors.username && (
                <p className="text-sm text-destructive">{errors.username.message}</p>
              )}
            </div>
            <div className="space-y-2">
              <Label htmlFor="password">Password</Label>
              <Input
                id="password"
                type="password"
                autoComplete="current-password"
                {...register("password")}
              />
              {errors.password && (
                <p className="text-sm text-destructive">{errors.password.message}</p>
              )}
            </div>

            {mutation.isError && (
              <Alert variant="destructive">
                <AlertDescription>
                  {mutation.error instanceof ApiError
                    ? mutation.error.message
                    : "Login failed."}
                </AlertDescription>
              </Alert>
            )}

            <Button type="submit" className="w-full" disabled={mutation.isPending}>
              {mutation.isPending ? "Signing in…" : "Sign in"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
