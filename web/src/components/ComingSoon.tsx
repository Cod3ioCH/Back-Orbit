import { Construction } from "lucide-react";
import { Badge } from "@/components/ui/badge";

interface ComingSoonProps {
  title: string;
  description: string;
}

export function ComingSoon({ title, description }: ComingSoonProps) {
  return (
    <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-24 text-center">
      <div className="mb-4 flex size-10 items-center justify-center rounded-full bg-muted">
        <Construction className="size-5 text-muted-foreground" aria-hidden="true" />
      </div>
      <div className="flex items-center gap-2">
        <h1 className="text-lg font-semibold">{title}</h1>
        <Badge variant="outline" className="font-normal text-muted-foreground">
          Coming soon
        </Badge>
      </div>
      <p className="mt-2 max-w-md text-sm text-muted-foreground">{description}</p>
    </div>
  );
}
