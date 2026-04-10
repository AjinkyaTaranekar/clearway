import { Clock3 } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';

interface CapacityReleaseTimerProps {
  cleanupIntervalMinutes?: number;
  orphanThresholdMinutes?: number;
  mode?: 'inline' | 'compact';
}

function formatMMSS(totalSeconds: number): string {
  const safe = Math.max(0, Math.floor(totalSeconds));
  const minutes = Math.floor(safe / 60);
  const seconds = safe % 60;
  return `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`;
}

function secondsUntilNextSweep(nowMs: number, cleanupIntervalMinutes: number): number {
  const intervalMs = cleanupIntervalMinutes * 60 * 1000;
  const elapsed = nowMs % intervalMs;
  const remainingMs = intervalMs - elapsed;
  return Math.max(1, Math.ceil(remainingMs / 1000));
}

export default function CapacityReleaseTimer({
  cleanupIntervalMinutes = 5,
  orphanThresholdMinutes = 5,
  mode = 'inline',
}: CapacityReleaseTimerProps) {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    const timer = window.setInterval(() => {
      setNow(Date.now());
    }, 1000);

    return () => window.clearInterval(timer);
  }, []);

  const nextSweepSeconds = useMemo(
    () => secondsUntilNextSweep(now, cleanupIntervalMinutes),
    [cleanupIntervalMinutes, now],
  );

  const worstCaseSeconds = nextSweepSeconds + (orphanThresholdMinutes * 60);
  const compact = mode === 'compact';

  return (
    <div
      className="rounded-lg"
      style={{
        background: '#FFF4E0',
        border: '1px solid #F1D7A5',
        padding: compact ? '8px 10px' : '10px 12px',
      }}
    >
      <div className="flex items-center gap-2" style={{ marginBottom: compact ? '4px' : '6px' }}>
        <Clock3 size={compact ? 14 : 15} color="#7A4500" />
        <span style={{ color: '#7A4500', fontSize: compact ? '0.75rem' : '0.8125rem', fontWeight: 600 }}>
          Demo auto-release timer
        </span>
        <span
          style={{
            color: '#7A4500',
            fontSize: compact ? '0.875rem' : '0.9375rem',
            fontWeight: 700,
            marginLeft: 'auto',
            fontFamily: 'var(--font-heading)',
          }}
        >
          {formatMMSS(nextSweepSeconds)}
        </span>
      </div>

      <p style={{ color: '#7A4500', fontSize: compact ? '0.75rem' : '0.8125rem', lineHeight: 1.5 }}>
        Next cleanup sweep in {formatMMSS(nextSweepSeconds)}. Worst-case stale hold reclaim in about {formatMMSS(worstCaseSeconds)}.
      </p>
    </div>
  );
}
