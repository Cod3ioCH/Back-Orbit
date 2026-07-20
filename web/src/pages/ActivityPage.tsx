import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Radio } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Badge } from "@/components/ui/badge";
import { api, type AuditEvent } from "@/lib/api";
import { formatDateTime } from "@/lib/format";

const MAX_LIVE_EVENTS = 200;

export function ActivityPage() {
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
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Activity</h1>
          <p className="text-sm text-muted-foreground">
            A live, append-only audit trail of security-relevant and user-visible actions.
          </p>
        </div>
        <Badge variant="outline" className="gap-1.5">
          <Radio className={connected ? "size-3 text-success" : "size-3 text-muted-foreground"} />
          {connected ? "Live" : "Connecting…"}
        </Badge>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Recent events</CardTitle>
        </CardHeader>
        <CardContent>
          {query.isLoading ? (
            <div className="space-y-2">
              {Array.from({ length: 6 }).map((_, i) => (
                <Skeleton key={i} className="h-10 w-full" />
              ))}
            </div>
          ) : events.length === 0 ? (
            <p className="text-sm text-muted-foreground">No activity recorded yet.</p>
          ) : (
            <ul className="divide-y">
              {events.map((event) => (
                <li key={event.id} className="flex flex-wrap items-center justify-between gap-2 py-3">
                  <div>
                    <div className="text-sm font-medium">{event.action}</div>
                    {event.targetType && (
                      <div className="text-xs text-muted-foreground">
                        {event.targetType}
                        {event.targetId ? ` · ${event.targetId}` : ""}
                      </div>
                    )}
                  </div>
                  <div className="text-xs text-muted-foreground">
                    {formatDateTime(event.createdAt)}
                  </div>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
