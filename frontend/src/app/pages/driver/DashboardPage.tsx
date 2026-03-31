import { format, parseISO } from 'date-fns';
import {
  AlertCircle,
  ArrowRight,
  Calendar,
  CheckCircle2,
  ChevronRight,
  Clock,
  MapPin,
  Navigation,
  Zap,
} from 'lucide-react';
import { useNavigate } from 'react-router';
import { StatusChip } from '../../components/ui/StatusChip';
import { useApp } from '../../context/AppContext';

export default function DriverDashboard() {
  const navigate = useNavigate();
  const { user, journeys, notifications } = useApp();

  const hour = new Date().getHours();
  const greeting = hour < 12 ? 'Good morning' : hour < 17 ? 'Good afternoon' : 'Good evening';
  const today = new Date().toLocaleDateString('en-GB', { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' });

  const activeJourney = journeys.find((j) => j.status === 'active');
  const nextApproved = journeys.find((j) => j.status === 'approved');
  const pendingCount = journeys.filter((j) => j.status === 'pending').length;
  const completedCount = journeys.filter((j) => j.status === 'completed').length;
  const totalCount = journeys.length;
  const recentNotifications = notifications.slice(0, 3);

  const highlightJourney = activeJourney || nextApproved;

  const formatTime = (ts: string) => {
    try { return format(parseISO(ts), 'HH:mm'); } catch { return ts; }
  };
  const formatDate = (ts: string) => {
    try { return format(parseISO(ts), 'EEE, d MMM'); } catch { return ts; }
  };

  const notifTypeColor = (type: string) => {
    if (type === 'success') return '#2E7D32';
    if (type === 'error') return '#B42318';
    if (type === 'warning') return '#A15C00';
    return '#1A4E80';
  };

  const notifTypeBg = (type: string) => {
    if (type === 'success') return '#E8F4ED';
    if (type === 'error') return '#FDECEA';
    if (type === 'warning') return '#FFF4E0';
    return '#E3EEFB';
  };

  return (
    <div className="p-5 lg:p-8 max-w-4xl mx-auto">
      {/* Header */}
      <div className="mb-8">
        <h1 style={{ fontFamily: 'var(--font-heading)', fontWeight: 700, color: '#1F2421', marginBottom: '4px' }}>
          {greeting}, {user?.name?.split(' ')[0]}.
        </h1>
        <p style={{ color: '#4E5953', fontSize: '0.9375rem' }}>
          {today} - Here's your journey overview.
        </p>
      </div>

      {/* Stats row */}
      <div className="grid grid-cols-3 gap-3 mb-6">
        {[
          { label: 'Total journeys', value: totalCount, icon: Calendar, color: '#2F6B55' },
          { label: 'Completed', value: completedCount, icon: CheckCircle2, color: '#2E7D32' },
          { label: 'Pending', value: pendingCount, icon: Clock, color: '#A15C00' },
        ].map((s) => (
          <div
            key={s.label}
            className="bg-white rounded-xl p-4"
            style={{ border: '1px solid var(--border)' }}
          >
            <div className="flex items-center justify-between mb-2">
              <div
                className="w-8 h-8 rounded-lg flex items-center justify-center"
                style={{ background: `${s.color}15` }}
              >
                <s.icon size={16} color={s.color} />
              </div>
            </div>
            <div style={{ fontFamily: 'var(--font-heading)', fontWeight: 700, fontSize: '1.625rem', color: '#1F2421', lineHeight: 1 }}>
              {s.value}
            </div>
            <div style={{ color: '#4E5953', fontSize: '0.8125rem', marginTop: '4px' }}>
              {s.label}
            </div>
          </div>
        ))}
      </div>

      {/* Current / next journey highlight */}
      {highlightJourney ? (
        <div
          className="bg-white rounded-xl p-5 mb-6 cursor-pointer hover:shadow-md transition-shadow"
          style={{ border: '1px solid var(--border)' }}
          onClick={() => navigate(`/driver/journeys/${highlightJourney.id}`)}
          role="button"
          tabIndex={0}
          onKeyDown={(e) => e.key === 'Enter' && navigate(`/driver/journeys/${highlightJourney.id}`)}
        >
          <div className="flex items-center justify-between mb-4">
            <div>
              <p style={{ color: '#4E5953', fontSize: '0.8125rem', fontWeight: 500, marginBottom: '4px', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
                {activeJourney ? 'Active journey' : 'Next journey'}
              </p>
              <div className="flex items-center gap-2">
                <StatusChip status={highlightJourney.status} />
                <span style={{ color: '#4E5953', fontSize: '0.875rem' }}>
                  {highlightJourney.id}
                </span>
              </div>
            </div>
            <ChevronRight size={20} color="#4E5953" />
          </div>

          <div className="flex items-center gap-3 mb-4">
            <div className="flex-1">
              <div className="flex items-center gap-2 mb-1">
                <div className="w-2 h-2 rounded-full" style={{ background: '#2F6B55' }} />
                <span style={{ color: '#1F2421', fontWeight: 500, fontSize: '0.9375rem' }}>
                  {highlightJourney.origin}
                </span>
              </div>
              <div
                className="ml-[13px] border-l-2 border-dashed h-4"
                style={{ borderColor: 'var(--border)' }}
              />
              <div className="flex items-center gap-2">
                <MapPin size={8} color="#B65C3A" fill="#B65C3A" />
                <span style={{ color: '#1F2421', fontWeight: 500, fontSize: '0.9375rem' }}>
                  {highlightJourney.destination}
                </span>
              </div>
            </div>
            <div className="text-right">
              <div style={{ fontFamily: 'var(--font-heading)', fontWeight: 700, fontSize: '1.5rem', color: '#1F2421', lineHeight: 1 }}>
                {formatTime(highlightJourney.departureTime)}
              </div>
              <div style={{ color: '#4E5953', fontSize: '0.8125rem' }}>
                {formatDate(highlightJourney.departureTime)}
              </div>
            </div>
          </div>

          <div className="flex items-center gap-4">
            <span style={{ color: '#4E5953', fontSize: '0.875rem' }}>
              🚗 {highlightJourney.vehicleType}
            </span>
            <span style={{ color: '#4E5953', fontSize: '0.875rem' }}>
              📏 {highlightJourney.distance}
            </span>
            <span style={{ color: '#4E5953', fontSize: '0.875rem' }}>
              ⏱ {highlightJourney.duration}
            </span>
          </div>
        </div>
      ) : (
        <div
          className="bg-white rounded-xl p-8 mb-6 text-center"
          style={{ border: '1px solid var(--border)' }}
        >
          <div
            className="w-14 h-14 rounded-full flex items-center justify-center mx-auto mb-3"
            style={{ background: '#F0EDE7' }}
          >
            <Navigation size={24} color="#4E5953" />
          </div>
          <p style={{ color: '#1F2421', fontWeight: 600, fontFamily: 'var(--font-heading)', marginBottom: '4px' }}>
            No upcoming journeys
          </p>
          <p style={{ color: '#4E5953', fontSize: '0.875rem', marginBottom: '16px' }}>
            Book a journey to get started.
          </p>
          <button
            onClick={() => navigate('/driver/book')}
            className="px-5 py-2.5 rounded-lg text-white text-sm transition-colors"
            style={{ background: '#2F6B55', fontWeight: 600 }}
            onMouseEnter={(e) => (e.currentTarget.style.background = '#245343')}
            onMouseLeave={(e) => (e.currentTarget.style.background = '#2F6B55')}
          >
            Book a journey
          </button>
        </div>
      )}

      {/* Quick actions */}
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 mb-6">
        <button
          onClick={() => navigate('/driver/book')}
          className="flex items-center gap-4 p-5 bg-white rounded-xl text-left hover:shadow-md transition-shadow"
          style={{ border: '1px solid var(--border)' }}
        >
          <div
            className="w-10 h-10 rounded-lg flex items-center justify-center flex-shrink-0"
            style={{ background: '#2F6B55' }}
          >
            <Navigation size={18} color="white" />
          </div>
          <div className="flex-1">
            <div style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem' }}>
              Book a journey
            </div>
            <div style={{ color: '#4E5953', fontSize: '0.8125rem' }}>
              Pre-book and check road availability
            </div>
          </div>
          <ArrowRight size={16} color="#4E5953" />
        </button>

        <button
          onClick={() => navigate('/driver/journeys')}
          className="flex items-center gap-4 p-5 bg-white rounded-xl text-left hover:shadow-md transition-shadow"
          style={{ border: '1px solid var(--border)' }}
        >
          <div
            className="w-10 h-10 rounded-lg flex items-center justify-center flex-shrink-0"
            style={{ background: '#F0EDE7' }}
          >
            <Zap size={18} color="#2F6B55" />
          </div>
          <div className="flex-1">
            <div style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem' }}>
              View all journeys
            </div>
            <div style={{ color: '#4E5953', fontSize: '0.8125rem' }}>
              Track current and past trips
            </div>
          </div>
          <ArrowRight size={16} color="#4E5953" />
        </button>
      </div>

      {/* Recent notifications */}
      {recentNotifications.length > 0 && (
        <div className="bg-white rounded-xl overflow-hidden" style={{ border: '1px solid var(--border)' }}>
          <div
            className="flex items-center justify-between px-5 py-3.5"
            style={{ borderBottom: '1px solid var(--border)' }}
          >
            <span style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem' }}>
              Recent updates
            </span>
            <button
              onClick={() => navigate('/driver/notifications')}
              style={{ color: '#2F6B55', fontSize: '0.875rem', fontWeight: 500 }}
            >
              See all
            </button>
          </div>
          <ul>
            {recentNotifications.map((n, i) => (
              <li
                key={n.id}
                className="flex items-start gap-3 px-5 py-4 cursor-pointer hover:bg-muted transition-colors"
                style={{ borderBottom: i < recentNotifications.length - 1 ? '1px solid var(--border)' : 'none' }}
                onClick={() => navigate('/driver/notifications')}
              >
                <div
                  className="w-8 h-8 rounded-full flex items-center justify-center flex-shrink-0 mt-0.5"
                  style={{ background: notifTypeBg(n.type) }}
                >
                  {n.type === 'success' ? (
                    <CheckCircle2 size={14} color={notifTypeColor(n.type)} />
                  ) : (
                    <AlertCircle size={14} color={notifTypeColor(n.type)} />
                  )}
                </div>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span style={{ fontWeight: n.read ? 400 : 600, color: '#1F2421', fontSize: '0.875rem' }}>
                      {n.title}
                    </span>
                    {!n.read && (
                      <span className="w-1.5 h-1.5 rounded-full flex-shrink-0" style={{ background: '#2F6B55' }} />
                    )}
                  </div>
                  <p
                    className="mt-0.5 line-clamp-1"
                    style={{ color: '#4E5953', fontSize: '0.8125rem' }}
                  >
                    {n.message}
                  </p>
                </div>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}