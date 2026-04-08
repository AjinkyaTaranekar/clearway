import {
  Activity,
  AlertCircle,
  BarChart2,
  ChevronRight,
  Globe,
  List,
  Map,
  TrendingDown,
  TrendingUp
} from 'lucide-react';
import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router';
import { toast } from 'sonner';
import { StatusChip } from '../../components/ui/StatusChip';
import { useApp } from '../../context/AppContext';
import { AdminAnalyticsResult, adminAnalytics } from '../../services/journeyApi';
import { getRegion } from '../../services/mapApi';

export default function AdminDashboardPage() {
  const navigate = useNavigate();
  const { adminJourneys, user } = useApp();

  // U-14: fetch real analytics from backend instead of using hardcoded mock data
  const [analytics, setAnalytics] = useState<AdminAnalyticsResult | null>(null);
  const [region, setRegion] = useState<string | null>(null);
  useEffect(() => {
    adminAnalytics('24h').then(setAnalytics).catch((err) => {
      toast.error('Analytics unavailable', {
        description: err instanceof Error ? err.message : 'Could not load dashboard stats.',
      });
    });
    getRegion().then(setRegion);
  }, []);

  const activeJourneys = adminJourneys.filter((j) => j.status === 'active');
  const pendingJourneys = adminJourneys.filter((j) => j.status === 'pending');
  const recentJourneys = adminJourneys.slice(0, 5);

  const stats = [
    {
      label: 'Total bookings',
      value: analytics ? analytics.total_journeys : '—',
      sub: 'Last 24 h',
      icon: List,
      color: '#2F6B55',
      bg: '#E8F4ED',
    },
    {
      label: 'Approval rate',
      value: analytics ? `${analytics.approval_rate.toFixed(1)}%` : '—',
      sub: 'Last 24 h',
      icon: TrendingUp,
      color: '#2E7D32',
      bg: '#E8F4ED',
    },
    {
      label: 'Active now',
      value: analytics ? analytics.active : '—',
      sub: 'On road',
      icon: Activity,
      color: '#1A4E80',
      bg: '#E3EEFB',
    },
    {
      label: 'Rejection rate',
      value: analytics ? `${analytics.rejection_rate.toFixed(1)}%` : '—',
      sub: 'Last 24 h',
      icon: TrendingDown,
      color: '#B42318',
      bg: '#FDECEA',
    },
  ];

  return (
    <div className="p-5 lg:p-8 max-w-5xl mx-auto">
      <div className="flex items-start justify-between mb-8">
        <div>
          <h1 style={{ fontFamily: 'var(--font-heading)', fontWeight: 700, color: '#1F2421', marginBottom: '4px' }}>
            Admin dashboard
          </h1>
          <p style={{ color: '#4E5953', fontSize: '0.9375rem' }}>
            Good morning, {user?.name?.split(' ')[0]}. Here's the system overview for today.
          </p>
        </div>
        {region && (
          <div
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-full"
            style={{ background: '#E3EEFB', border: '1px solid #B8D4F0', flexShrink: 0 }}
          >
            <Globe size={13} color="#1A4E80" />
            <span style={{ color: '#1A4E80', fontSize: '0.8125rem', fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.04em' }}>
              {region}
            </span>
          </div>
        )}
      </div>

      {/* KPI cards */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3 mb-6">
        {stats.map((s) => (
          <div
            key={s.label}
            className="bg-white rounded-xl p-4"
            style={{ border: '1px solid var(--border)' }}
          >
            <div
              className="w-9 h-9 rounded-lg flex items-center justify-center mb-3"
              style={{ background: s.bg }}
            >
              <s.icon size={17} color={s.color} />
            </div>
            <div
              style={{
                fontFamily: 'var(--font-heading)',
                fontWeight: 700,
                fontSize: '1.625rem',
                color: '#1F2421',
                lineHeight: 1,
                marginBottom: '4px',
              }}
            >
              {s.value}
            </div>
            <div style={{ color: '#1F2421', fontSize: '0.8125rem', fontWeight: 500 }}>{s.label}</div>
            <div style={{ color: '#4E5953', fontSize: '0.75rem' }}>{s.sub}</div>
          </div>
        ))}
      </div>

      {/* Alerts */}
      {pendingJourneys.length > 0 && (
        <div
          className="flex items-center gap-3 p-4 rounded-xl mb-5"
          style={{ background: '#FFF4E0', border: '1px solid #F0D8A0' }}
        >
          <AlertCircle size={18} color="#A15C00" className="flex-shrink-0" />
          <p style={{ color: '#7A4500', fontSize: '0.9375rem', flex: 1 }}>
            <strong>{pendingJourneys.length} journey{pendingJourneys.length > 1 ? 's' : ''}</strong> pending system evaluation. These will resolve automatically.
          </p>
          <button
            onClick={() => navigate('/admin/journeys')}
            style={{ color: '#A15C00', fontSize: '0.875rem', fontWeight: 500, whiteSpace: 'nowrap' }}
          >
            View all →
          </button>
        </div>
      )}

      {/* Quick nav cards */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 mb-6">
        {[
          {
            to: '/admin/journeys',
            icon: List,
            label: 'All journeys',
            desc: `${adminJourneys.length} total across all drivers`,
            color: '#2F6B55',
            bg: '#E8F4ED',
          },
          {
            to: '/admin/analytics',
            icon: BarChart2,
            label: 'Analytics',
            desc: 'Booking trends and regional stats',
            color: '#1A4E80',
            bg: '#E3EEFB',
          },
          {
            to: '/admin/map',
            icon: Map,
            label: 'Traffic map',
            desc: 'Live road segment occupancy',
            color: '#B65C3A',
            bg: '#FAEEE7',
          },
        ].map((card) => (
          <button
            key={card.to}
            onClick={() => navigate(card.to)}
            className="flex items-start gap-4 p-4 bg-white rounded-xl text-left hover:shadow-md transition-shadow"
            style={{ border: '1px solid var(--border)' }}
          >
            <div
              className="w-10 h-10 rounded-lg flex items-center justify-center flex-shrink-0"
              style={{ background: card.bg }}
            >
              <card.icon size={18} color={card.color} />
            </div>
            <div className="flex-1">
              <div style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem', marginBottom: '2px' }}>
                {card.label}
              </div>
              <div style={{ color: '#4E5953', fontSize: '0.8125rem' }}>{card.desc}</div>
            </div>
            <ChevronRight size={16} color="#4E5953" className="self-center flex-shrink-0" />
          </button>
        ))}
      </div>

      {/* Active journeys */}
      {activeJourneys.length > 0 && (
        <div className="bg-white rounded-xl overflow-hidden mb-5" style={{ border: '1px solid var(--border)' }}>
          <div
            className="flex items-center justify-between px-5 py-3.5"
            style={{ borderBottom: '1px solid var(--border)' }}
          >
            <div className="flex items-center gap-2">
              <div className="w-2 h-2 rounded-full" style={{ background: '#1A4E80' }} />
              <span style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem' }}>
                Active journeys ({activeJourneys.length})
              </span>
            </div>
            <button
              onClick={() => navigate('/admin/journeys')}
              style={{ color: '#2F6B55', fontSize: '0.875rem', fontWeight: 500 }}
            >
              View all
            </button>
          </div>
          <ul>
            {activeJourneys.map((j, i) => (
              <li
                key={j.id}
                className="flex items-center justify-between px-5 py-3 cursor-pointer hover:bg-muted transition-colors"
                style={{ borderBottom: i < activeJourneys.length - 1 ? '1px solid var(--border)' : 'none' }}
                onClick={() => navigate(`/admin/journeys/${j.id}`)}
              >
                <div>
                  <div style={{ fontWeight: 500, color: '#1F2421', fontSize: '0.875rem' }}>
                    {j.driverName} - {j.origin} → {j.destination}
                  </div>
                  <div style={{ color: '#4E5953', fontSize: '0.8125rem' }}>
                    {j.id} · {j.vehicleType}
                  </div>
                </div>
                <StatusChip status={j.status} size="sm" />
              </li>
            ))}
          </ul>
        </div>
      )}

      {/* Recent journeys */}
      <div className="bg-white rounded-xl overflow-hidden" style={{ border: '1px solid var(--border)' }}>
        <div
          className="flex items-center justify-between px-5 py-3.5"
          style={{ borderBottom: '1px solid var(--border)' }}
        >
          <span style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem' }}>
            Recent journeys
          </span>
          <button
            onClick={() => navigate('/admin/journeys')}
            style={{ color: '#2F6B55', fontSize: '0.875rem', fontWeight: 500 }}
          >
            View all
          </button>
        </div>
        <ul>
          {recentJourneys.map((j, i) => (
            <li
              key={j.id}
              className="flex items-center justify-between px-5 py-3.5 cursor-pointer hover:bg-muted transition-colors"
              style={{ borderBottom: i < recentJourneys.length - 1 ? '1px solid var(--border)' : 'none' }}
              onClick={() => navigate(`/admin/journeys/${j.id}`)}
            >
              <div className="flex-1 min-w-0">
                <div style={{ fontWeight: 500, color: '#1F2421', fontSize: '0.875rem' }}>
                  {j.driverName}
                </div>
                <div style={{ color: '#4E5953', fontSize: '0.8125rem' }}>
                  {j.origin} → {j.destination} · {j.id}
                </div>
              </div>
              <StatusChip status={j.status} size="sm" />
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
