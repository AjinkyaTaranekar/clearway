import { useEffect, useState } from 'react';
import { AlertTriangle, CheckCircle, PlusCircle, X, XCircle } from 'lucide-react';
import { format, parseISO } from 'date-fns';
import {
  createClosure,
  CreateClosureParams,
  listClosures,
  SegmentClosure,
} from '../../services/capacityApi';

const SEGMENT_IDS = [
  'seg_city_north',
  'seg_north_airport',
  'seg_city_east',
  'seg_east_airport',
  'seg_city_riverside',
  'seg_riverside_south',
  'seg_south_industrial',
  'seg_industrial_east',
  'seg_city_west',
  'seg_west_port',
  'seg_port_south',
  'seg_west_northfield',
  'seg_northfield_north',
];

function formatTs(ts: string | undefined) {
  if (!ts) return '—';
  try { return format(parseISO(ts), 'dd MMM yyyy · HH:mm'); } catch { return ts; }
}

export default function SegmentClosuresPage() {
  const [closures, setClosures] = useState<SegmentClosure[]>([]);
  const [loading, setLoading] = useState(true);
  const [fetchError, setFetchError] = useState<string | null>(null);
  const [showForm, setShowForm] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [submitSuccess, setSubmitSuccess] = useState(false);

  const [form, setForm] = useState<CreateClosureParams>({
    segment_id: '',
    reason: '',
    starts_at: '',
    ends_at: '',
  });

  useEffect(() => {
    setLoading(true);
    listClosures()
      .then(setClosures)
      .catch((e) => setFetchError(e.message ?? 'Failed to load closures'))
      .finally(() => setLoading(false));
  }, []);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    setSubmitError(null);
    setSubmitSuccess(false);

    try {
      const params: CreateClosureParams = {
        segment_id: form.segment_id,
        reason: form.reason,
        starts_at: new Date(form.starts_at).toISOString(),
      };
      if (form.ends_at) {
        params.ends_at = new Date(form.ends_at).toISOString();
      }
      const created = await createClosure(params);
      setClosures((prev) => [created, ...prev]);
      setShowForm(false);
      setSubmitSuccess(true);
      setForm({ segment_id: '', reason: '', starts_at: '', ends_at: '' });
      setTimeout(() => setSubmitSuccess(false), 4000);
    } catch (err: any) {
      setSubmitError(err.message ?? 'Failed to create closure');
    } finally {
      setSubmitting(false);
    }
  };

  const activeCount = closures.filter((c) => c.is_active).length;

  return (
    <div className="p-5 lg:p-8 max-w-5xl">
      {/* Header */}
      <div className="flex items-center justify-between mb-7 gap-3 flex-wrap">
        <div>
          <h1 style={{ fontFamily: 'var(--font-heading)', fontWeight: 700, color: '#1F2421', marginBottom: '4px' }}>
            Segment closures
          </h1>
          <p style={{ color: '#4E5953', fontSize: '0.9375rem' }}>
            {activeCount} active closure{activeCount !== 1 ? 's' : ''} across all road segments.
          </p>
        </div>
        <button
          onClick={() => { setShowForm((s) => !s); setSubmitError(null); }}
          className="flex items-center gap-2 px-4 py-2.5 rounded-lg text-sm transition-opacity"
          style={{ background: '#2F6B55', color: 'white', fontWeight: 600 }}
        >
          <PlusCircle size={16} />
          New closure
        </button>
      </div>

      {/* Success banner */}
      {submitSuccess && (
        <div
          className="flex items-center gap-3 p-4 rounded-xl mb-5"
          style={{ background: '#F0FDF4', border: '1px solid #86EFAC' }}
        >
          <CheckCircle size={18} color="#16A34A" className="flex-shrink-0" />
          <p style={{ color: '#15803D', fontSize: '0.9375rem' }}>Closure created successfully.</p>
        </div>
      )}

      {/* Create form */}
      {showForm && (
        <form
          onSubmit={handleSubmit}
          className="bg-white rounded-xl p-5 mb-6"
          style={{ border: '1px solid var(--border)' }}
        >
          <div className="flex items-center justify-between mb-4">
            <h2 style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', fontSize: '1rem' }}>
              New closure
            </h2>
            <button
              type="button"
              onClick={() => setShowForm(false)}
              className="p-1 rounded-md hover:bg-muted transition-colors"
            >
              <X size={18} color="#4E5953" />
            </button>
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            {/* Segment ID */}
            <div>
              <label className="block text-sm mb-1.5" style={{ color: '#1F2421', fontWeight: 500 }}>
                Segment <span style={{ color: '#C0392B' }}>*</span>
              </label>
              <select
                required
                value={form.segment_id}
                onChange={(e) => setForm((f) => ({ ...f, segment_id: e.target.value }))}
                className="w-full px-3.5 py-2.5 rounded-lg outline-none appearance-none"
                style={{ border: '1.5px solid var(--border)', color: form.segment_id ? '#1F2421' : '#9CA3AF', background: '#F8F6F2' }}
              >
                <option value="" disabled>Select segment…</option>
                {SEGMENT_IDS.map((id) => (
                  <option key={id} value={id}>{id}</option>
                ))}
              </select>
            </div>

            {/* Reason */}
            <div>
              <label className="block text-sm mb-1.5" style={{ color: '#1F2421', fontWeight: 500 }}>
                Reason <span style={{ color: '#C0392B' }}>*</span>
              </label>
              <input
                required
                type="text"
                placeholder="e.g. Road maintenance"
                value={form.reason}
                onChange={(e) => setForm((f) => ({ ...f, reason: e.target.value }))}
                className="w-full px-3.5 py-2.5 rounded-lg outline-none"
                style={{ border: '1.5px solid var(--border)', color: '#1F2421', background: '#F8F6F2' }}
                onFocus={(e) => (e.target.style.borderColor = '#2F6B55')}
                onBlur={(e) => (e.target.style.borderColor = 'var(--border)')}
              />
            </div>

            {/* Starts at */}
            <div>
              <label className="block text-sm mb-1.5" style={{ color: '#1F2421', fontWeight: 500 }}>
                Starts at <span style={{ color: '#C0392B' }}>*</span>
              </label>
              <input
                required
                type="datetime-local"
                value={form.starts_at}
                onChange={(e) => setForm((f) => ({ ...f, starts_at: e.target.value }))}
                className="w-full px-3.5 py-2.5 rounded-lg outline-none"
                style={{ border: '1.5px solid var(--border)', color: '#1F2421', background: '#F8F6F2' }}
                onFocus={(e) => (e.target.style.borderColor = '#2F6B55')}
                onBlur={(e) => (e.target.style.borderColor = 'var(--border)')}
              />
            </div>

            {/* Ends at */}
            <div>
              <label className="block text-sm mb-1.5" style={{ color: '#1F2421', fontWeight: 500 }}>
                Ends at <span style={{ color: '#4E5953', fontWeight: 400 }}>(optional — leave blank for indefinite)</span>
              </label>
              <input
                type="datetime-local"
                value={form.ends_at}
                onChange={(e) => setForm((f) => ({ ...f, ends_at: e.target.value }))}
                className="w-full px-3.5 py-2.5 rounded-lg outline-none"
                style={{ border: '1.5px solid var(--border)', color: '#1F2421', background: '#F8F6F2' }}
                onFocus={(e) => (e.target.style.borderColor = '#2F6B55')}
                onBlur={(e) => (e.target.style.borderColor = 'var(--border)')}
              />
            </div>
          </div>

          {submitError && (
            <div
              className="flex items-center gap-2 mt-4 p-3 rounded-lg"
              style={{ background: '#FEF2F2', border: '1px solid #FECACA' }}
            >
              <XCircle size={16} color="#DC2626" className="flex-shrink-0" />
              <span className="text-sm" style={{ color: '#B91C1C' }}>{submitError}</span>
            </div>
          )}

          <div className="flex gap-3 mt-5">
            <button
              type="submit"
              disabled={submitting}
              className="px-5 py-2.5 rounded-lg text-sm transition-opacity"
              style={{
                background: '#2F6B55',
                color: 'white',
                fontWeight: 600,
                opacity: submitting ? 0.6 : 1,
                cursor: submitting ? 'not-allowed' : 'pointer',
              }}
            >
              {submitting ? 'Creating…' : 'Create closure'}
            </button>
            <button
              type="button"
              onClick={() => setShowForm(false)}
              className="px-5 py-2.5 rounded-lg text-sm"
              style={{ border: '1.5px solid var(--border)', color: '#4E5953', background: 'white' }}
            >
              Cancel
            </button>
          </div>
        </form>
      )}

      {/* Error state */}
      {fetchError && (
        <div
          className="flex items-center gap-3 p-4 rounded-xl mb-5"
          style={{ background: '#FEF2F2', border: '1px solid #FECACA' }}
        >
          <AlertTriangle size={18} color="#DC2626" className="flex-shrink-0" />
          <p style={{ color: '#B91C1C', fontSize: '0.9375rem' }}>{fetchError}</p>
        </div>
      )}

      {/* Table */}
      {!loading && !fetchError && (
        <div className="bg-white rounded-xl overflow-hidden hidden md:block" style={{ border: '1px solid var(--border)' }}>
          <table className="w-full">
            <thead>
              <tr style={{ borderBottom: '1px solid var(--border)', background: '#F8F6F2' }}>
                {['Closure ID', 'Segment', 'Reason', 'Starts', 'Ends', 'Status'].map((col) => (
                  <th
                    key={col}
                    className="px-4 py-3 text-left"
                    style={{ color: '#4E5953', fontSize: '0.8125rem', fontWeight: 500, fontFamily: 'var(--font-body)' }}
                  >
                    {col}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {closures.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-4 py-12 text-center">
                    <p style={{ color: '#4E5953', fontSize: '0.9375rem' }}>No closures recorded yet.</p>
                  </td>
                </tr>
              ) : (
                closures.map((c, i) => (
                  <tr
                    key={c.closure_id}
                    style={{ borderBottom: i < closures.length - 1 ? '1px solid var(--border)' : 'none' }}
                  >
                    <td className="px-4 py-3.5">
                      <span style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', fontSize: '0.875rem' }}>
                        {c.closure_id}
                      </span>
                    </td>
                    <td className="px-4 py-3.5">
                      <div style={{ fontWeight: 500, color: '#1F2421', fontSize: '0.875rem' }}>{c.segment_name}</div>
                      <div style={{ color: '#4E5953', fontSize: '0.75rem', fontFamily: 'var(--font-heading)' }}>{c.segment_id}</div>
                    </td>
                    <td className="px-4 py-3.5" style={{ maxWidth: '220px' }}>
                      <span style={{ color: '#1F2421', fontSize: '0.875rem' }} className="truncate block">{c.reason}</span>
                    </td>
                    <td className="px-4 py-3.5">
                      <span style={{ color: '#1F2421', fontSize: '0.875rem' }}>{formatTs(c.starts_at)}</span>
                    </td>
                    <td className="px-4 py-3.5">
                      <span style={{ color: c.ends_at ? '#1F2421' : '#9CA3AF', fontSize: '0.875rem' }}>
                        {c.ends_at ? formatTs(c.ends_at) : 'Indefinite'}
                      </span>
                    </td>
                    <td className="px-4 py-3.5">
                      <ClosureStatusChip isActive={c.is_active} />
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      )}

      {/* Mobile cards */}
      {!loading && !fetchError && (
        <div className="md:hidden space-y-2.5">
          {closures.length === 0 ? (
            <div className="bg-white rounded-xl p-8 text-center" style={{ border: '1px solid var(--border)' }}>
              <p style={{ color: '#4E5953' }}>No closures recorded yet.</p>
            </div>
          ) : (
            closures.map((c) => (
              <div key={c.closure_id} className="bg-white rounded-xl p-4" style={{ border: '1px solid var(--border)' }}>
                <div className="flex items-start justify-between gap-2 mb-2">
                  <ClosureStatusChip isActive={c.is_active} />
                  <span style={{ color: '#4E5953', fontSize: '0.8125rem', fontFamily: 'var(--font-heading)' }}>{c.closure_id}</span>
                </div>
                <div style={{ fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem', marginBottom: '2px' }}>{c.segment_name}</div>
                <div style={{ color: '#4E5953', fontSize: '0.875rem', marginBottom: '8px' }}>{c.reason}</div>
                <div className="flex items-center gap-2 flex-wrap" style={{ fontSize: '0.8125rem', color: '#4E5953' }}>
                  <span>{formatTs(c.starts_at)}</span>
                  <span style={{ color: '#D9D2C7' }}>→</span>
                  <span>{c.ends_at ? formatTs(c.ends_at) : 'Indefinite'}</span>
                </div>
              </div>
            ))
          )}
        </div>
      )}

      {/* Loading skeleton */}
      {loading && (
        <div className="space-y-3">
          {[1, 2, 3].map((n) => (
            <div
              key={n}
              className="bg-white rounded-xl h-16 animate-pulse"
              style={{ border: '1px solid var(--border)' }}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function ClosureStatusChip({ isActive }: { isActive: boolean }) {
  return (
    <span
      className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium"
      style={{
        background: isActive ? '#FFF4E0' : '#F3F4F6',
        color: isActive ? '#A15C00' : '#6B7280',
        border: `1px solid ${isActive ? '#F0D8A0' : '#E5E7EB'}`,
      }}
    >
      {isActive ? (
        <AlertTriangle size={11} />
      ) : (
        <CheckCircle size={11} />
      )}
      {isActive ? 'Active' : 'Resolved'}
    </span>
  );
}
