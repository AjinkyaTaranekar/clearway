import { useState, useMemo } from 'react';
import { useNavigate } from 'react-router';
import { useApp } from '../../context/AppContext';
import { StatusChip } from '../../components/ui/StatusChip';
import { JourneyStatus, Region } from '../../types';
import {
  Search, ChevronRight, ChevronUp, ChevronDown, Filter,
  Navigation, ArrowUpDown,
} from 'lucide-react';
import { format, parseISO } from 'date-fns';

type SortField = 'id' | 'driverName' | 'departureTime' | 'status' | 'region';
type SortDir = 'asc' | 'desc';

const STATUS_OPTS: { label: string; value: JourneyStatus | 'all' }[] = [
  { label: 'All statuses', value: 'all' },
  { label: 'Active', value: 'active' },
  { label: 'Approved', value: 'approved' },
  { label: 'Pending', value: 'pending' },
  { label: 'Completed', value: 'completed' },
  { label: 'Rejected', value: 'rejected' },
  { label: 'Cancelled', value: 'cancelled' },
];

const REGION_OPTS: { label: string; value: Region | 'all' }[] = [
  { label: 'All regions', value: 'all' },
  { label: 'North', value: 'North' },
  { label: 'South', value: 'South' },
  { label: 'East', value: 'East' },
  { label: 'West', value: 'West' },
  { label: 'Central', value: 'Central' },
];

const PAGE_SIZE = 10;

const vehicleIcons: Record<string, string> = {
  Car: '🚗', Van: '🚐', Motorcycle: '🏍️', HGV: '🚛',
};

export default function AllJourneysPage() {
  const navigate = useNavigate();
  const { adminJourneys } = useApp();

  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState<JourneyStatus | 'all'>('all');
  const [regionFilter, setRegionFilter] = useState<Region | 'all'>('all');
  const [sortField, setSortField] = useState<SortField>('departureTime');
  const [sortDir, setSortDir] = useState<SortDir>('desc');
  const [page, setPage] = useState(1);

  const handleSort = (field: SortField) => {
    if (sortField === field) {
      setSortDir((d) => (d === 'asc' ? 'desc' : 'asc'));
    } else {
      setSortField(field);
      setSortDir('asc');
    }
    setPage(1);
  };

  const filtered = useMemo(() => {
    const q = search.toLowerCase();
    return adminJourneys
      .filter((j) => {
        const matchStatus = statusFilter === 'all' || j.status === statusFilter;
        const matchRegion = regionFilter === 'all' || j.region === regionFilter;
        const matchSearch =
          !q ||
          j.id.toLowerCase().includes(q) ||
          j.driverName.toLowerCase().includes(q) ||
          j.origin.toLowerCase().includes(q) ||
          j.destination.toLowerCase().includes(q);
        return matchStatus && matchRegion && matchSearch;
      })
      .sort((a, b) => {
        let av: string = '';
        let bv: string = '';
        if (sortField === 'id') { av = a.id; bv = b.id; }
        else if (sortField === 'driverName') { av = a.driverName; bv = b.driverName; }
        else if (sortField === 'departureTime') { av = a.departureTime; bv = b.departureTime; }
        else if (sortField === 'status') { av = a.status; bv = b.status; }
        else if (sortField === 'region') { av = a.region; bv = b.region; }
        return sortDir === 'asc' ? av.localeCompare(bv) : bv.localeCompare(av);
      });
  }, [adminJourneys, search, statusFilter, regionFilter, sortField, sortDir]);

  const totalPages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
  const paginated = filtered.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE);

  const SortIcon = ({ field }: { field: SortField }) => {
    if (sortField !== field) return <ArrowUpDown size={13} color="#D9D2C7" />;
    return sortDir === 'asc'
      ? <ChevronUp size={13} color="#2F6B55" />
      : <ChevronDown size={13} color="#2F6B55" />;
  };

  const formatDeparture = (ts: string) => {
    try { return format(parseISO(ts), 'dd MMM · HH:mm'); } catch { return ts; }
  };

  return (
    <div className="p-5 lg:p-8">
      {/* Header */}
      <div className="flex items-center justify-between mb-7 gap-3 flex-wrap">
        <div>
          <h1 style={{ fontFamily: 'var(--font-heading)', fontWeight: 700, color: '#1F2421', marginBottom: '4px' }}>
            All journeys
          </h1>
          <p style={{ color: '#4E5953', fontSize: '0.9375rem' }}>
            {filtered.length} journeys across all drivers and regions.
          </p>
        </div>
      </div>

      {/* Filters */}
      <div className="bg-white rounded-xl p-4 mb-5 flex flex-col gap-3" style={{ border: '1px solid var(--border)' }}>
        <div className="flex gap-3 flex-wrap items-center">
          {/* Search */}
          <div className="relative flex-1 min-w-[200px]">
            <Search size={16} color="#4E5953" className="absolute left-3.5 top-1/2 -translate-y-1/2 pointer-events-none" />
            <input
              type="text"
              placeholder="Search driver, ID, or location…"
              value={search}
              onChange={(e) => { setSearch(e.target.value); setPage(1); }}
              className="w-full pl-10 pr-4 py-2.5 rounded-lg outline-none"
              style={{ border: '1.5px solid var(--border)', color: '#1F2421', background: '#F8F6F2' }}
              onFocus={(e) => (e.target.style.borderColor = '#2F6B55')}
              onBlur={(e) => (e.target.style.borderColor = 'var(--border)')}
            />
          </div>

          {/* Status filter */}
          <select
            value={statusFilter}
            onChange={(e) => { setStatusFilter(e.target.value as JourneyStatus | 'all'); setPage(1); }}
            className="px-3 py-2.5 rounded-lg outline-none appearance-none cursor-pointer"
            style={{ border: '1.5px solid var(--border)', color: '#1F2421', background: '#F8F6F2', minWidth: '150px' }}
          >
            {STATUS_OPTS.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
          </select>

          {/* Region filter */}
          <select
            value={regionFilter}
            onChange={(e) => { setRegionFilter(e.target.value as Region | 'all'); setPage(1); }}
            className="px-3 py-2.5 rounded-lg outline-none appearance-none cursor-pointer"
            style={{ border: '1.5px solid var(--border)', color: '#1F2421', background: '#F8F6F2', minWidth: '150px' }}
          >
            {REGION_OPTS.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
          </select>
        </div>

        {/* Active filter chips */}
        {(statusFilter !== 'all' || regionFilter !== 'all' || search) && (
          <div className="flex items-center gap-2 flex-wrap">
            <span style={{ color: '#4E5953', fontSize: '0.8125rem' }}>Active filters:</span>
            {search && (
              <button
                onClick={() => { setSearch(''); setPage(1); }}
                className="flex items-center gap-1 px-2.5 py-1 rounded-full text-xs"
                style={{ background: '#E8F4ED', color: '#1E6639', fontWeight: 500 }}
              >
                "{search}" ×
              </button>
            )}
            {statusFilter !== 'all' && (
              <button
                onClick={() => { setStatusFilter('all'); setPage(1); }}
                className="flex items-center gap-1 px-2.5 py-1 rounded-full text-xs"
                style={{ background: '#E8F4ED', color: '#1E6639', fontWeight: 500 }}
              >
                {statusFilter} ×
              </button>
            )}
            {regionFilter !== 'all' && (
              <button
                onClick={() => { setRegionFilter('all'); setPage(1); }}
                className="flex items-center gap-1 px-2.5 py-1 rounded-full text-xs"
                style={{ background: '#E8F4ED', color: '#1E6639', fontWeight: 500 }}
              >
                {regionFilter} ×
              </button>
            )}
          </div>
        )}
      </div>

      {/* Table (desktop) */}
      <div className="bg-white rounded-xl overflow-hidden hidden md:block" style={{ border: '1px solid var(--border)' }}>
        <table className="w-full">
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border)', background: '#F8F6F2' }}>
              {[
                { label: 'Journey ID', field: 'id' as SortField },
                { label: 'Driver', field: 'driverName' as SortField },
                { label: 'Route', field: null },
                { label: 'Status', field: 'status' as SortField },
                { label: 'Departure', field: 'departureTime' as SortField },
                { label: 'Region', field: 'region' as SortField },
                { label: '', field: null },
              ].map((col) => (
                <th
                  key={col.label}
                  className="px-4 py-3 text-left"
                  style={{ color: '#4E5953', fontSize: '0.8125rem', fontWeight: 500, fontFamily: 'var(--font-body)' }}
                >
                  {col.field ? (
                    <button
                      onClick={() => handleSort(col.field!)}
                      className="flex items-center gap-1.5 hover:opacity-80 transition-opacity"
                    >
                      {col.label}
                      <SortIcon field={col.field} />
                    </button>
                  ) : col.label}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {paginated.length === 0 ? (
              <tr>
                <td colSpan={7} className="px-4 py-12 text-center">
                  <div className="flex flex-col items-center gap-2">
                    <Filter size={24} color="#D9D2C7" />
                    <p style={{ color: '#4E5953', fontSize: '0.9375rem' }}>No journeys match your filters.</p>
                    <button
                      onClick={() => { setSearch(''); setStatusFilter('all'); setRegionFilter('all'); }}
                      style={{ color: '#2F6B55', fontSize: '0.875rem', fontWeight: 500 }}
                    >
                      Clear all filters
                    </button>
                  </div>
                </td>
              </tr>
            ) : (
              paginated.map((j, i) => (
                <tr
                  key={j.id}
                  onClick={() => navigate(`/admin/journeys/${j.id}`)}
                  className="cursor-pointer hover:bg-muted transition-colors"
                  style={{ borderBottom: i < paginated.length - 1 ? '1px solid var(--border)' : 'none' }}
                >
                  <td className="px-4 py-3.5">
                    <span style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', fontSize: '0.875rem' }}>
                      {j.id}
                    </span>
                  </td>
                  <td className="px-4 py-3.5">
                    <div style={{ color: '#1F2421', fontWeight: 500, fontSize: '0.875rem' }}>{j.driverName}</div>
                    <div style={{ color: '#4E5953', fontSize: '0.75rem' }}>{j.driverId}</div>
                  </td>
                  <td className="px-4 py-3.5" style={{ maxWidth: '200px' }}>
                    <div style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }} className="truncate">{j.origin}</div>
                    <div style={{ color: '#4E5953', fontSize: '0.8125rem' }} className="truncate">→ {j.destination}</div>
                  </td>
                  <td className="px-4 py-3.5">
                    <StatusChip status={j.status} size="sm" />
                  </td>
                  <td className="px-4 py-3.5">
                    <span style={{ color: '#1F2421', fontSize: '0.875rem' }}>{formatDeparture(j.departureTime)}</span>
                  </td>
                  <td className="px-4 py-3.5">
                    <span style={{ color: '#4E5953', fontSize: '0.875rem' }}>{j.region}</span>
                  </td>
                  <td className="px-4 py-3.5">
                    <ChevronRight size={16} color="#4E5953" />
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      {/* Cards (mobile) */}
      <div className="md:hidden space-y-2.5">
        {paginated.length === 0 ? (
          <div className="bg-white rounded-xl p-8 text-center" style={{ border: '1px solid var(--border)' }}>
            <Filter size={24} color="#D9D2C7" className="mx-auto mb-2" />
            <p style={{ color: '#4E5953' }}>No journeys match your filters.</p>
          </div>
        ) : (
          paginated.map((j) => (
            <div
              key={j.id}
              onClick={() => navigate(`/admin/journeys/${j.id}`)}
              className="bg-white rounded-xl p-4 cursor-pointer hover:shadow-md transition-shadow"
              style={{ border: '1px solid var(--border)' }}
            >
              <div className="flex items-start justify-between gap-2 mb-2">
                <StatusChip status={j.status} size="sm" />
                <span style={{ color: '#4E5953', fontSize: '0.8125rem' }}>{j.id}</span>
              </div>
              <div style={{ fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem', marginBottom: '4px' }}>{j.driverName}</div>
              <div style={{ color: '#4E5953', fontSize: '0.875rem' }}>{j.origin} → {j.destination}</div>
              <div className="flex items-center gap-3 mt-2 pt-2" style={{ borderTop: '1px solid var(--border)' }}>
                <span style={{ color: '#4E5953', fontSize: '0.8125rem' }}>{vehicleIcons[j.vehicleType]} {j.vehicleType}</span>
                <span style={{ color: '#D9D2C7' }}>·</span>
                <span style={{ color: '#4E5953', fontSize: '0.8125rem' }}>{j.region}</span>
                <span style={{ color: '#D9D2C7' }}>·</span>
                <span style={{ color: '#4E5953', fontSize: '0.8125rem' }}>{formatDeparture(j.departureTime)}</span>
              </div>
            </div>
          ))
        )}
      </div>

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="flex items-center justify-between mt-5 flex-wrap gap-3">
          <span style={{ color: '#4E5953', fontSize: '0.875rem' }}>
            Showing {(page - 1) * PAGE_SIZE + 1}–{Math.min(page * PAGE_SIZE, filtered.length)} of {filtered.length}
          </span>
          <div className="flex items-center gap-1.5">
            <button
              onClick={() => setPage((p) => Math.max(1, p - 1))}
              disabled={page === 1}
              className="px-3 py-1.5 rounded-lg text-sm transition-colors"
              style={{
                border: '1.5px solid var(--border)',
                color: page === 1 ? '#D9D2C7' : '#4E5953',
                background: 'white',
                cursor: page === 1 ? 'not-allowed' : 'pointer',
              }}
            >
              Previous
            </button>
            {Array.from({ length: totalPages }, (_, i) => i + 1)
              .filter((p) => p === 1 || p === totalPages || Math.abs(p - page) <= 1)
              .map((p, idx, arr) => (
                <>
                  {idx > 0 && arr[idx - 1] !== p - 1 && (
                    <span key={`ellipsis-${p}`} style={{ color: '#4E5953', fontSize: '0.875rem' }}>…</span>
                  )}
                  <button
                    key={p}
                    onClick={() => setPage(p)}
                    className="w-9 h-9 rounded-lg text-sm transition-colors"
                    style={{
                      border: '1.5px solid',
                      borderColor: p === page ? '#2F6B55' : 'var(--border)',
                      background: p === page ? '#2F6B55' : 'white',
                      color: p === page ? 'white' : '#4E5953',
                      fontWeight: p === page ? 600 : 400,
                    }}
                  >
                    {p}
                  </button>
                </>
              ))}
            <button
              onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
              disabled={page === totalPages}
              className="px-3 py-1.5 rounded-lg text-sm transition-colors"
              style={{
                border: '1.5px solid var(--border)',
                color: page === totalPages ? '#D9D2C7' : '#4E5953',
                background: 'white',
                cursor: page === totalPages ? 'not-allowed' : 'pointer',
              }}
            >
              Next
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
