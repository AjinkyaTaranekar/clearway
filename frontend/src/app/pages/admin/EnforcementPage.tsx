import { useState } from 'react';
import { ShieldCheck, ShieldAlert, Search, Clock } from 'lucide-react';
import { enforcementVerify, EnforcementVerifyResult } from '../../services/journeyApi';
import { format, parseISO } from 'date-fns';

function formatTs(ts: string | undefined) {
  if (!ts) return '—';
  try { return format(parseISO(ts), 'dd MMM yyyy · HH:mm:ss'); } catch { return ts; }
}

export default function EnforcementPage() {
  const [segmentId, setSegmentId] = useState('');
  const [vehiclePlate, setVehiclePlate] = useState('');
  const [useNow, setUseNow] = useState(true);
  const [timestampInput, setTimestampInput] = useState('');
  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState<EnforcementVerifyResult | null>(null);
  const [error, setError] = useState<string | null>(null);

  const handleVerify = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!segmentId.trim()) return;

    setLoading(true);
    setResult(null);
    setError(null);

    try {
      const ts = useNow ? undefined : (timestampInput ? new Date(timestampInput).toISOString() : undefined);
      const data = await enforcementVerify({
        segmentId: segmentId.trim(),
        vehiclePlate: vehiclePlate.trim() || undefined,
        timestamp: ts,
      });
      setResult(data);
    } catch (err: any) {
      setError(err.message ?? 'Verification failed');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="p-5 lg:p-8 max-w-2xl">
      {/* Header */}
      <div className="mb-7">
        <h1 style={{ fontFamily: 'var(--font-heading)', fontWeight: 700, color: '#1F2421', marginBottom: '4px' }}>
          Enforcement verify
        </h1>
        <p style={{ color: '#4E5953', fontSize: '0.9375rem' }}>
          Check whether a vehicle on a road segment has a valid active journey booking.
        </p>
      </div>

      {/* Form */}
      <form
        onSubmit={handleVerify}
        className="bg-white rounded-xl p-5 mb-6"
        style={{ border: '1px solid var(--border)' }}
      >
        <div className="space-y-4">
          {/* Segment ID */}
          <div>
            <label
              htmlFor="segment_id"
              className="block text-sm mb-1.5"
              style={{ color: '#1F2421', fontWeight: 500 }}
            >
              Segment ID <span style={{ color: '#C0392B' }}>*</span>
            </label>
            <input
              id="segment_id"
              type="text"
              placeholder="e.g. seg_m50"
              value={segmentId}
              onChange={(e) => setSegmentId(e.target.value)}
              required
              className="w-full px-3.5 py-2.5 rounded-lg outline-none"
              style={{ border: '1.5px solid var(--border)', color: '#1F2421', background: '#F8F6F2' }}
              onFocus={(e) => (e.target.style.borderColor = '#2F6B55')}
              onBlur={(e) => (e.target.style.borderColor = 'var(--border)')}
            />
          </div>

          {/* Vehicle plate */}
          <div>
            <label
              htmlFor="vehicle_plate"
              className="block text-sm mb-1.5"
              style={{ color: '#1F2421', fontWeight: 500 }}
            >
              Vehicle plate <span style={{ color: '#4E5953', fontWeight: 400 }}>(optional)</span>
            </label>
            <input
              id="vehicle_plate"
              type="text"
              placeholder="e.g. 241-D-12345"
              value={vehiclePlate}
              onChange={(e) => setVehiclePlate(e.target.value)}
              className="w-full px-3.5 py-2.5 rounded-lg outline-none"
              style={{ border: '1.5px solid var(--border)', color: '#1F2421', background: '#F8F6F2' }}
              onFocus={(e) => (e.target.style.borderColor = '#2F6B55')}
              onBlur={(e) => (e.target.style.borderColor = 'var(--border)')}
            />
          </div>

          {/* Timestamp */}
          <div>
            <label className="block text-sm mb-1.5" style={{ color: '#1F2421', fontWeight: 500 }}>
              Timestamp
            </label>
            <div className="flex items-center gap-3 mb-2">
              <label className="flex items-center gap-2 cursor-pointer">
                <input
                  type="radio"
                  checked={useNow}
                  onChange={() => setUseNow(true)}
                  className="accent-green-700"
                />
                <span className="text-sm flex items-center gap-1.5" style={{ color: '#1F2421' }}>
                  <Clock size={14} color="#4E5953" /> Now
                </span>
              </label>
              <label className="flex items-center gap-2 cursor-pointer">
                <input
                  type="radio"
                  checked={!useNow}
                  onChange={() => setUseNow(false)}
                  className="accent-green-700"
                />
                <span className="text-sm" style={{ color: '#1F2421' }}>Specific time</span>
              </label>
            </div>
            {!useNow && (
              <input
                type="datetime-local"
                value={timestampInput}
                onChange={(e) => setTimestampInput(e.target.value)}
                className="w-full px-3.5 py-2.5 rounded-lg outline-none"
                style={{ border: '1.5px solid var(--border)', color: '#1F2421', background: '#F8F6F2' }}
                onFocus={(e) => (e.target.style.borderColor = '#2F6B55')}
                onBlur={(e) => (e.target.style.borderColor = 'var(--border)')}
              />
            )}
          </div>

          {/* Submit */}
          <button
            type="submit"
            disabled={loading || !segmentId.trim()}
            className="w-full flex items-center justify-center gap-2 py-2.5 rounded-lg text-sm transition-opacity"
            style={{
              background: '#2F6B55',
              color: 'white',
              fontWeight: 600,
              opacity: loading || !segmentId.trim() ? 0.6 : 1,
              cursor: loading || !segmentId.trim() ? 'not-allowed' : 'pointer',
            }}
          >
            <Search size={16} />
            {loading ? 'Verifying…' : 'Verify'}
          </button>
        </div>
      </form>

      {/* Error */}
      {error && (
        <div
          className="rounded-xl p-4 mb-4 flex items-start gap-3"
          style={{ background: '#FEF2F2', border: '1px solid #FECACA' }}
        >
          <ShieldAlert size={18} color="#DC2626" className="flex-shrink-0 mt-0.5" />
          <div>
            <div className="text-sm font-medium" style={{ color: '#DC2626' }}>Verification error</div>
            <div className="text-sm mt-0.5" style={{ color: '#B91C1C' }}>{error}</div>
          </div>
        </div>
      )}

      {/* Result */}
      {result && (
        <div
          className="rounded-xl p-5"
          style={{
            border: `1.5px solid ${result.authorized ? '#86EFAC' : '#FECACA'}`,
            background: result.authorized ? '#F0FDF4' : '#FEF2F2',
          }}
        >
          {/* Status banner */}
          <div className="flex items-center gap-3 mb-4">
            {result.authorized ? (
              <div
                className="w-10 h-10 rounded-full flex items-center justify-center flex-shrink-0"
                style={{ background: '#DCFCE7' }}
              >
                <ShieldCheck size={20} color="#16A34A" />
              </div>
            ) : (
              <div
                className="w-10 h-10 rounded-full flex items-center justify-center flex-shrink-0"
                style={{ background: '#FEE2E2' }}
              >
                <ShieldAlert size={20} color="#DC2626" />
              </div>
            )}
            <div>
              <div
                className="font-semibold"
                style={{
                  color: result.authorized ? '#15803D' : '#DC2626',
                  fontFamily: 'var(--font-heading)',
                  fontSize: '1rem',
                }}
              >
                {result.authorized ? 'Authorised' : 'Not authorised'}
              </div>
              <div className="text-xs mt-0.5" style={{ color: result.authorized ? '#166534' : '#B91C1C' }}>
                Checked at {formatTs(result.timestamp)}
              </div>
            </div>
          </div>

          {/* Details grid */}
          <div
            className="rounded-lg p-4 space-y-2.5"
            style={{ background: 'white', border: '1px solid var(--border)' }}
          >
            <Row label="Segment" value={result.segment_id} />
            {result.authorized && (
              <>
                <Row label="Journey ID" value={result.journey_id ?? '—'} mono />
                <Row label="Driver ID" value={result.driver_id ?? '—'} mono />
                <Row label="Status" value={result.status ?? '—'} />
                <Row label="Window start" value={formatTs(result.time_window_start)} />
                <Row label="Window end" value={formatTs(result.time_window_end)} />
              </>
            )}
            {vehiclePlate && (
              <Row label="Plate (hint)" value={vehiclePlate} />
            )}
          </div>
        </div>
      )}
    </div>
  );
}

function Row({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-start justify-between gap-4">
      <span className="text-sm flex-shrink-0" style={{ color: '#4E5953', minWidth: '110px' }}>{label}</span>
      <span
        className="text-sm text-right"
        style={{ color: '#1F2421', fontWeight: 500, fontFamily: mono ? 'var(--font-heading)' : 'inherit' }}
      >
        {value}
      </span>
    </div>
  );
}
