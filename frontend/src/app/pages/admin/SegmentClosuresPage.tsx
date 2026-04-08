import { AlertTriangle, Clock3, PlusCircle, RefreshCw } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { toast } from 'sonner';
import {
  createSegmentClosure,
  listActiveClosures,
  listCapacitySegments,
} from '../../services/capacityApi';
import { CapacitySegment, SegmentClosure } from '../../types';

function titleCase(value: string): string {
  if (!value) return value;
  return value.charAt(0).toUpperCase() + value.slice(1);
}

function formatDateTime(iso: string): string {
  const dt = new Date(iso);
  if (Number.isNaN(dt.getTime())) return iso;
  return dt.toLocaleString('en-GB', {
    day: '2-digit',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  });
}

export default function SegmentClosuresPage() {
  const [segments, setSegments] = useState<CapacitySegment[]>([]);
  const [closures, setClosures] = useState<SegmentClosure[]>([]);
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [segmentID, setSegmentID] = useState('');
  const [durationMinutes, setDurationMinutes] = useState(60);
  const [reason, setReason] = useState('');

  const segmentMap = useMemo(() => {
    return new Map(segments.map((seg) => [seg.segment_id, seg]));
  }, [segments]);

  const loadData = async () => {
    setLoading(true);
    setError(null);
    try {
      const [segmentData, closureData] = await Promise.all([
        listCapacitySegments(),
        listActiveClosures(),
      ]);
      setSegments(segmentData);
      setClosures(closureData);
      if (!segmentID && segmentData.length > 0) {
        setSegmentID(segmentData[0].segment_id);
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load closure data.';
      setError(message);
      toast.error('Closure data unavailable', { description: message });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void loadData();
  }, []);

  const handleCreateClosure = async () => {
    if (!segmentID) {
      toast.error('Select a segment first.');
      return;
    }

    if (!reason.trim()) {
      toast.error('Reason is required for closures.');
      return;
    }

    setSubmitting(true);
    try {
      await createSegmentClosure({
        segment_id: segmentID,
        duration_minutes: durationMinutes,
        reason: reason.trim(),
      });

      toast.success('Segment closure created', {
        description: `Segment ${segmentID} blocked for ${durationMinutes} minutes.`,
      });

      setReason('');
      const fresh = await listActiveClosures();
      setClosures(fresh);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Closure creation failed.';
      toast.error('Closure creation failed', { description: message });
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="p-5 lg:p-8 max-w-6xl mx-auto">
      <div className="flex items-start justify-between gap-3 mb-5 flex-wrap">
        <div>
          <h1 style={{ fontFamily: 'var(--font-heading)', fontWeight: 700, color: '#1F2421', marginBottom: '4px' }}>
            Segment closures
          </h1>
          <p style={{ color: '#4E5953', fontSize: '0.9375rem' }}>
            Block specific road segments immediately for a fixed duration. New bookings crossing closed segments are rejected.
          </p>
        </div>
        <button
          type="button"
          onClick={() => void loadData()}
          disabled={loading}
          className="flex items-center gap-2 text-sm"
          style={{ color: '#4E5953' }}
        >
          <RefreshCw size={13} className={loading ? 'animate-spin' : ''} />
          Refresh
        </button>
      </div>

      {error && (
        <div className="rounded-xl p-4 mb-5 flex items-start gap-3" style={{ background: '#FEF2F2', border: '1px solid #FECACA' }}>
          <AlertTriangle size={16} color="#DC2626" className="mt-0.5" />
          <div>
            <div style={{ color: '#DC2626', fontWeight: 600 }}>Failed to load closures</div>
            <div style={{ color: '#B91C1C', fontSize: '0.875rem' }}>{error}</div>
          </div>
        </div>
      )}

      <div className="grid lg:grid-cols-3 gap-5">
        <div className="lg:col-span-1 bg-white rounded-2xl p-5" style={{ border: '1px solid var(--border)' }}>
          <h2 style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', marginBottom: '14px' }}>
            Create closure
          </h2>

          <div className="space-y-4">
            <div>
              <label className="block mb-1.5" style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>
                Segment
              </label>
              <select
                value={segmentID}
                onChange={(e) => setSegmentID(e.target.value)}
                className="w-full rounded-lg px-3 py-2 text-sm"
                style={{ border: '1.5px solid var(--border)', color: '#1F2421' }}
              >
                {segments.map((segment) => (
                  <option key={segment.segment_id} value={segment.segment_id}>
                    {segment.segment_name} ({segment.segment_id})
                  </option>
                ))}
              </select>
            </div>

            <div>
              <label className="block mb-1.5" style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>
                Duration (minutes)
              </label>
              <input
                type="number"
                min={15}
                max={10080}
                step={15}
                value={durationMinutes}
                onChange={(e) => setDurationMinutes(Math.max(15, Number(e.target.value) || 15))}
                className="w-full rounded-lg px-3 py-2 text-sm"
                style={{ border: '1.5px solid var(--border)', color: '#1F2421' }}
              />
            </div>

            <div>
              <label className="block mb-1.5" style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>
                Reason
              </label>
              <textarea
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                rows={3}
                placeholder="e.g. accident response or urgent maintenance"
                className="w-full rounded-lg px-3 py-2 text-sm resize-none"
                style={{ border: '1.5px solid var(--border)', color: '#1F2421' }}
              />
            </div>

            <button
              type="button"
              onClick={() => void handleCreateClosure()}
              disabled={submitting || loading}
              className="w-full py-2.5 rounded-lg text-white text-sm flex items-center justify-center gap-2"
              style={{ background: '#2F6B55', fontWeight: 600 }}
            >
              <PlusCircle size={16} />
              {submitting ? 'Creating closure...' : 'Create closure'}
            </button>
          </div>
        </div>

        <div className="lg:col-span-2 bg-white rounded-2xl overflow-hidden" style={{ border: '1px solid var(--border)' }}>
          <div className="px-5 py-4" style={{ borderBottom: '1px solid var(--border)' }}>
            <h2 style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421' }}>
              Active closures
            </h2>
          </div>

          {loading ? (
            <div className="p-5" style={{ color: '#4E5953', fontSize: '0.875rem' }}>Loading closures...</div>
          ) : closures.length === 0 ? (
            <div className="p-5" style={{ color: '#4E5953', fontSize: '0.875rem' }}>
              No active closures.
            </div>
          ) : (
            <div className="divide-y" style={{ borderColor: 'var(--border)' }}>
              {closures.map((closure) => {
                const segment = segmentMap.get(closure.segment_id);
                return (
                  <div key={closure.closure_id} className="p-4">
                    <div className="flex items-start justify-between gap-3 flex-wrap">
                      <div>
                        <div style={{ color: '#1F2421', fontWeight: 600 }}>
                          {segment?.segment_name ?? closure.segment_id}
                        </div>
                        <div style={{ color: '#4E5953', fontSize: '0.8125rem' }}>
                          {closure.segment_id} • {titleCase(segment?.region ?? '')}
                        </div>
                      </div>
                      <span
                        className="px-2.5 py-1 rounded-full text-xs"
                        style={{ background: '#FDECEA', color: '#B42318', fontWeight: 600 }}
                      >
                        Active
                      </span>
                    </div>

                    <div className="mt-3 flex items-center gap-2" style={{ color: '#4E5953', fontSize: '0.875rem' }}>
                      <Clock3 size={14} />
                      {formatDateTime(closure.start_time)} to {formatDateTime(closure.end_time)}
                    </div>

                    <div className="mt-2" style={{ color: '#1F2421', fontSize: '0.875rem' }}>
                      {closure.reason}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
