import { format, parseISO } from 'date-fns';
import { ChevronRight, Filter, Navigation, Search } from 'lucide-react';
import { useState } from 'react';
import { useNavigate } from 'react-router';
import { StatusChip } from '../../components/ui/StatusChip';
import { useApp } from '../../context/AppContext';
import { JourneyStatus } from '../../types';

const STATUS_FILTERS: { label: string; value: JourneyStatus | 'all' }[] = [
  { label: 'All', value: 'all' },
  { label: 'Active', value: 'active' },
  { label: 'Approved', value: 'approved' },
  { label: 'Pending', value: 'pending' },
  { label: 'Completed', value: 'completed' },
  { label: 'Rejected', value: 'rejected' },
  { label: 'Cancelled', value: 'cancelled' },
];

export default function MyJourneysPage() {
  const navigate = useNavigate();
  const { journeys } = useApp();
  const [statusFilter, setStatusFilter] = useState<JourneyStatus | 'all'>('all');
  const [search, setSearch] = useState('');

  const filtered = journeys.filter((j) => {
    const matchStatus = statusFilter === 'all' || j.status === statusFilter;
    const q = search.toLowerCase();
    const matchSearch =
      !q ||
      j.origin.toLowerCase().includes(q) ||
      j.destination.toLowerCase().includes(q) ||
      j.id.toLowerCase().includes(q);
    return matchStatus && matchSearch;
  });

  const formatTime = (ts: string) => {
    try { return format(parseISO(ts), 'HH:mm, EEE d MMM'); } catch { return ts; }
  };

  return (
    <div className="p-5 lg:p-8 max-w-3xl mx-auto">
      <div className="flex items-center justify-between mb-7">
        <div>
          <h1 style={{ fontFamily: 'var(--font-heading)', fontWeight: 700, color: '#1F2421', marginBottom: '4px' }}>
            My journeys
          </h1>
          <p style={{ color: '#4E5953', fontSize: '0.9375rem' }}>
            {journeys.length} total - view, manage, and track your trips.
          </p>
        </div>
        <button
          onClick={() => navigate('/driver/book')}
          className="hidden sm:flex items-center gap-2 px-4 py-2.5 rounded-lg text-white text-sm transition-colors"
          style={{ background: '#2F6B55', fontWeight: 600 }}
          onMouseEnter={(e) => (e.currentTarget.style.background = '#245343')}
          onMouseLeave={(e) => (e.currentTarget.style.background = '#2F6B55')}
        >
          <Navigation size={15} /> Book journey
        </button>
      </div>

      {/* Search & filters */}
      <div className="space-y-3 mb-5">
        <div className="relative">
          <Search size={16} color="#4E5953" className="absolute left-3.5 top-1/2 -translate-y-1/2 pointer-events-none" />
          <input
            type="text"
            placeholder="Search by origin, destination, or ID…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-full pl-10 pr-4 py-2.5 bg-white rounded-lg outline-none"
            style={{ border: '1.5px solid var(--border)', color: '#1F2421' }}
            onFocus={(e) => (e.target.style.borderColor = '#2F6B55')}
            onBlur={(e) => (e.target.style.borderColor = 'var(--border)')}
          />
        </div>

        <div className="flex gap-2 overflow-x-auto pb-1">
          {STATUS_FILTERS.map((f) => (
            <button
              key={f.value}
              onClick={() => setStatusFilter(f.value)}
              className="flex-shrink-0 px-3.5 py-1.5 rounded-full text-sm transition-all"
              style={{
                background: statusFilter === f.value ? '#2F6B55' : 'white',
                color: statusFilter === f.value ? 'white' : '#4E5953',
                border: statusFilter === f.value ? '1.5px solid #2F6B55' : '1.5px solid var(--border)',
                fontWeight: statusFilter === f.value ? 600 : 400,
              }}
            >
              {f.label}
            </button>
          ))}
        </div>
      </div>

      {/* Journey list */}
      {filtered.length === 0 ? (
        <div
          className="bg-white rounded-xl p-10 text-center"
          style={{ border: '1px solid var(--border)' }}
        >
          <div
            className="w-12 h-12 rounded-full flex items-center justify-center mx-auto mb-3"
            style={{ background: '#F0EDE7' }}
          >
            <Filter size={20} color="#4E5953" />
          </div>
          <p style={{ color: '#1F2421', fontWeight: 600, fontFamily: 'var(--font-heading)', marginBottom: '4px' }}>
            No journeys found
          </p>
          <p style={{ color: '#4E5953', fontSize: '0.875rem' }}>
            Try adjusting your filters or search.
          </p>
        </div>
      ) : (
        <div className="space-y-2.5">
          {filtered.map((journey) => (
            <div
              key={journey.id}
              onClick={() => navigate(`/driver/journeys/${journey.id}`)}
              className="bg-white rounded-xl p-4 cursor-pointer hover:shadow-md transition-shadow"
              style={{ border: '1px solid var(--border)' }}
              role="button"
              tabIndex={0}
              onKeyDown={(e) => e.key === 'Enter' && navigate(`/driver/journeys/${journey.id}`)}
            >
              <div className="flex items-start justify-between gap-3">
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 mb-2 flex-wrap">
                    <StatusChip status={journey.status} size="sm" />
                    <span style={{ color: '#4E5953', fontSize: '0.8125rem' }}>{journey.id}</span>
                  </div>

                  <div className="flex items-center gap-2 mb-1">
                    <div className="w-2 h-2 rounded-full flex-shrink-0" style={{ background: '#2F6B55' }} />
                    <span style={{ color: '#1F2421', fontWeight: 500, fontSize: '0.9375rem' }}>
                      {journey.origin}
                    </span>
                  </div>
                  <div className="ml-[13px] border-l-2 border-dashed h-3.5" style={{ borderColor: 'var(--border)' }} />
                  <div className="flex items-center gap-2">
                    <div className="w-2 h-2 rounded-full flex-shrink-0" style={{ background: '#B65C3A' }} />
                    <span style={{ color: '#1F2421', fontWeight: 500, fontSize: '0.9375rem' }}>
                      {journey.destination}
                    </span>
                  </div>
                </div>

                <div className="text-right flex-shrink-0">
                  <div style={{ fontFamily: 'var(--font-heading)', fontWeight: 700, color: '#1F2421', fontSize: '1rem' }}>
                    {formatTime(journey.departureTime).split(',')[0]}
                  </div>
                  <div style={{ color: '#4E5953', fontSize: '0.75rem' }}>
                    {formatTime(journey.departureTime).split(',')[1]?.trim()}
                  </div>
                </div>

                <ChevronRight size={18} color="#4E5953" className="flex-shrink-0 self-center" />
              </div>

              <div className="flex items-center gap-3 mt-3 pt-3" style={{ borderTop: '1px solid var(--border)' }}>
                <span style={{ color: '#4E5953', fontSize: '0.8125rem' }}>
                  Vehicle: {journey.vehicleType}
                </span>
                <span style={{ color: '#D9D2C7' }}>·</span>
                <span style={{ color: '#4E5953', fontSize: '0.8125rem' }}>{journey.distance}</span>
                <span style={{ color: '#D9D2C7' }}>·</span>
                <span style={{ color: '#4E5953', fontSize: '0.8125rem' }}>{journey.duration}</span>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
