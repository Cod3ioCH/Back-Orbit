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

const schema = z
  .object({
    username: z.string().min(3, "At least 3 characters").max(64),
    password: z.string().min(12, "At least 12 characters"),
    confirmPassword: z.string(),
  })
  .refine((data) => data.password === data.confirmPassword, {
    message: "Passwords do not match",
    path: ["confirmPassword"],
  });

type FormValues = z.infer<typeof schema>;

export function SetupPage() {
  const navigate = useNavigate();
  const { refresh, setupComplete, isLoading } = useAuth();
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<FormValues>({ resolver: zodResolver(schema) });

  const mutation = useMutation({
    mutationFn: (values: FormValues) => api.setupAdmin(values.username, values.password),
    onSuccess: async () => {
      await refresh();
      navigate("/", { replace: true });
    },
  });

  // Redirect via <Navigate> rather than calling navigate() here: this runs
  // during render, and triggering a router state update from inside render
  // is a side effect React warns about.
  if (!isLoading && setupComplete) {
    return <Navigate to="/login" replace />;
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-muted/30 p-4">
      <Card className="w-full max-w-sm">
        {/* See LoginPage: CardHeader is a grid, so the icon needs its own
            centering wrapper to line up with the centred text. */}
        <CardHeader className="text-center">
          <div className="flex justify-center">
            <OrbitIcon className="mb-2 size-8 text-primary" aria-hidden="true" />
          </div>
          <CardTitle>Welcome to Back-Orbit</CardTitle>
          <CardDescription>
            Create the administrator account to get started.
          </CardDescription>
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
                autoComplete="new-password"
                {...register("password")}
              />
              {errors.password && (
                <p className="text-sm text-destructive">{errors.password.message}</p>
              )}
            </div>
            <div className="space-y-2">
              <Label htmlFor="confirmPassword">Confirm password</Label>
              <Input
                id="confirmPassword"
                type="password"
                autoComplete="new-password"
                {...register("confirmPassword")}
              />
              {errors.confirmPassword && (
                <p className="text-sm text-destructive">{errors.confirmPassword.message}</p>
              )}
            </div>

            {mutation.isError && (
              <Alert variant="destructive">
                <AlertDescription>
                  {mutation.error instanceof ApiError
                    ? mutation.error.message
                    : "Failed to create the administrator account."}
                </AlertDescription>
              </Alert>
            )}

            <Button type="submit" className="w-full" disabled={mutation.isPending}>
              {mutation.isPending ? "Creating account…" : "Create administrator account"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
