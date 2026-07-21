import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Radio } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Badge } from "@/components/ui/badge";
import { Timestamp } from "@/components/Timestamp";
import { PageHeader } from "@/components/PageHeader";
import { EmptyState } from "@/components/EmptyState";
import { api, type AuditEvent } from "@/lib/api";
import { describeEvent, eventDetail, TONE_CLASSES } from "@/lib/events";
import { usePageTitle } from "@/hooks/usePageTitle";
import { cn } from "@/lib/utils";

const MAX_LIVE_EVENTS = 200;

export function ActivityPage() {
  usePageTitle("Activity");

  const query = useQuery({ queryKey: ["audit", 100], queryFn: () => api.listAudit(100) });
  const [liveEvents, setLiveEvents] = useState<AuditEvent[]>([]);
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    const source = new EventSource("/api/v1/activity/stream");

    source.onopen = () => setConnected(true);
    source.onerror = () => setConnected(false);
    source.onmessage = (message) => {
      try {
        const event = JSON.parse(message.data) as AuditEvent;
        setLiveEvents((prev) => [event, ...prev].slice(0, MAX_LIVE_EVENTS));
      } catch {
        // Ignore malformed events rather than crashing the activity feed.
      }
    };

    return () => source.close();
  }, []);

  const seenIds = new Set(liveEvents.map((e) => e.id));
  const historyEvents = (query.data ?? []).filter((e) => !seenIds.has(e.id));
  const events = [...liveEvents, ...historyEvents];

  return (
    <div className="space-y-6">
      <PageHeader
        title="Activity"
        description="Every security-relevant and user-visible action, newest first."
        actions={
          <Badge variant="outline" className="gap-1.5 font-normal">
            <Radio
              className={cn("size-3", connected ? "text-success" : "text-muted-foreground")}
              aria-hidden="true"
            />
            {connected ? "Live" : "Connecting…"}
          </Badge>
        }
      />

      <Card>
        <CardContent className="p-0">
          {query.isLoading ? (
            <div className="space-y-4 p-6">
              {Array.from({ length: 6 }).map((_, i) => (
                <Skeleton key={i} className="h-8 w-full" />
              ))}
            </div>
          ) : events.length === 0 ? (
            <EmptyState
              title="Nothing has happened yet"
              description="Actions you take in Back-Orbit — signing in, registering projects, running backups — are recorded here."
            />
          ) : (
            <ul className="divide-y">
              {events.map((event) => {
                const { label, icon: Icon, tone } = describeEvent(event);
                const detail = eventDetail(event);

                return (
                  <li
                    key={event.id}
                    className="flex items-center gap-3 px-4 py-3 sm:px-6"
                  >
                    <Icon
                      className={cn("size-4 shrink-0", TONE_CLASSES[tone])}
                      aria-hidden="true"
                    />
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-sm font-medium">{label}</div>
                      {detail && (
                        <div className="truncate text-xs text-muted-foreground">{detail}</div>
                      )}
                    </div>
                    <Timestamp
                      iso={event.createdAt}
                      className="shrink-0 text-xs text-muted-foreground"
                    />
                  </li>
                );
              })}
            </ul>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
