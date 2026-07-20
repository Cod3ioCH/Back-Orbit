import { Construction } from "lucide-react";

interface ComingSoonProps {
  title: string;
  description: string;
}

export function ComingSoon({ title, description }: ComingSoonProps) {
  return (
    <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-24 text-center">
      <Construction className="mb-4 size-8 text-muted-foreground" aria-hidden="true" />
      <h1 className="text-lg font-semibold">{title}</h1>
      <p className="mt-2 max-w-md text-sm text-muted-foreground">{description}</p>
    </div>
  );
}
