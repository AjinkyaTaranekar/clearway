import { Activity, AlertCircle, CheckCircle2, Clock, TrendingDown, TrendingUp, XCircle } from 'lucide-react';
import { useEffect, useState } from 'react';
import {
  Area,
  AreaChart,
  CartesianGrid,
  Cell,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';
import { toast } from 'sonner';
import { AdminAnalyticsResult, adminAnalytics } from '../../services/journeyApi';

type TimeWindow = '1h' | '24h' | '7d';

const TIME_WINDOWS: { label: string; value: TimeWindow }[] = [
  { label: 'Last hour', value: '1h' },
  { label: 'Last 24 hours', value: '24h' },
  { label: 'Last 7 days', value: '7d' },
];

const PIE_COLORS = ['#2F6B55', '#B42318', '#D9A441', '#4E5953'];

const CustomTooltip = ({ active, payload, label }: any) => {
  if (!active || !payload?.length) return null;
  return (
    <div className="bg-white rounded-xl p-3 shadow-lg" style={{ border: '1px solid var(--border)' }}>
      <p style={{ color: '#4E5953', fontSize: '0.8125rem', marginBottom: '6px' }}>{label}</p>
      {payload.map((p: any) => (
        <div key={p.name} className="flex items-center gap-2">
          <div className="w-2 h-2 rounded-full" style={{ background: p.color }} />
          <span style={{ color: '#1F2421', fontSize: '0.8125rem', fontWeight: 500 }}>{p.name}: {p.value}</span>
        </div>
      ))}
    </div>
  );
};

export default function AnalyticsPage() {
  const [window, setWindow] = useState<TimeWindow>('24h');
  const [kpis, setKpis] = useState<AdminAnalyticsResult | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setLoading(true);
    setError(null);
    adminAnalytics(window)
      .then(setKpis)
      .catch((err) => {
        setKpis(null);
        const msg = err instanceof Error ? err.message : 'Could not load analytics data.';
        setError(msg);
        toast.error('Analytics unavailable', { description: msg });
      })
      .finally(() => setLoading(false));
  }, [window]);

  const fmt = (n: number | undefined, decimals = 0) =>
    n === undefined ? '-' : decimals ? n.toFixed(decimals) : String(n);

  const pieData = kpis
    ? [
        { name: 'Approved', value: kpis.approved },
        { name: 'Rejected', value: kpis.rejected },
        { name: 'Cancelled', value: kpis.cancelled },
        { name: 'Active', value: kpis.active },
      ]
    : [];

  const statCards = [
    {
      label: 'Total bookings',
      value: fmt(kpis?.total_journeys),
      icon: Activity,
      color: '#2F6B55',
      bg: '#E8F4ED',
      sub: kpis ? `${kpis.window} window` : '-',
    },
    {
      label: 'Approval rate',
      value: kpis ? `${kpis.approval_rate.toFixed(1)}%` : '-',
      icon: TrendingUp,
      color: '#2E7D32',
      bg: '#E8F4ED',
      sub: `${fmt(kpis?.approved)} approved`,
    },
    {
      label: 'Rejection rate',
      value: kpis ? `${kpis.rejection_rate.toFixed(1)}%` : '-',
      icon: TrendingDown,
      color: '#B42318',
      bg: '#FDECEA',
      sub: `${fmt(kpis?.rejected)} rejected`,
    },
    {
      label: 'Active journeys',
      value: fmt(kpis?.active),
      icon: CheckCircle2,
      color: '#1A4E80',
      bg: '#E3EEFB',
      sub: 'Right now',
    },
    {
      label: 'Cancellations',
      value: fmt(kpis?.cancelled),
      icon: XCircle,
      color: '#B65C3A',
      bg: '#FAEEE7',
      sub: `${fmt(kpis?.expired)} expired`,
    },
    {
      label: 'Completed',
      value: fmt(kpis?.completed),
      icon: Clock,
      color: '#4E5953',
      bg: '#F0EDE7',
      sub: 'Finished journeys',
    },
  ];

  return (
    <div className="p-5 lg:p-8 max-w-6xl mx-auto">
      {/* Header */}
      <div className="flex items-start justify-between gap-4 mb-7 flex-wrap">
        <div>
          <h1 style={{ fontFamily: 'var(--font-heading)', fontWeight: 700, color: '#1F2421', marginBottom: '4px' }}>
            Analytics
          </h1>
          <p style={{ color: '#4E5953', fontSize: '0.9375rem' }}>
            System health and booking performance overview.
          </p>
        </div>

        <div className="flex rounded-lg p-1" style={{ background: '#F0EDE7' }}>
          {TIME_WINDOWS.map((tw) => (
            <button
              key={tw.value}
              onClick={() => setWindow(tw.value)}
              className="px-3 py-1.5 rounded-md text-sm transition-all duration-150"
              style={{
                background: window === tw.value ? 'white' : 'transparent',
                color: window === tw.value ? '#1F2421' : '#4E5953',
                fontWeight: window === tw.value ? 600 : 400,
                boxShadow: window === tw.value ? '0 1px 3px rgba(0,0,0,0.08)' : 'none',
              }}
            >
              {tw.label}
            </button>
          ))}
        </div>
      </div>

      {error && (
        <div
          className="flex items-center gap-3 p-3.5 rounded-xl mb-5"
          style={{ background: '#FEF3F2', border: '1px solid #FECACA' }}
        >
          <AlertCircle size={16} color="#B42318" />
          <p style={{ color: '#B42318', fontSize: '0.875rem' }}>{error}</p>
        </div>
      )}

      {/* KPI Cards */}
      <div className="grid grid-cols-2 lg:grid-cols-3 gap-3 mb-6">
        {statCards.map((s) => (
          <div key={s.label} className="bg-white rounded-xl p-4" style={{ border: '1px solid var(--border)' }}>
            <div className="w-9 h-9 rounded-lg flex items-center justify-center mb-3" style={{ background: s.bg }}>
              <s.icon size={17} color={s.color} />
            </div>
            <div
              style={{
                fontFamily: 'var(--font-heading)',
                fontWeight: 700,
                fontSize: loading ? '1rem' : '1.625rem',
                color: loading ? '#4E5953' : '#1F2421',
                lineHeight: 1,
                marginBottom: '4px',
              }}
            >
              {loading ? 'Loading…' : s.value}
            </div>
            <div style={{ color: '#1F2421', fontSize: '0.8125rem', fontWeight: 500 }}>{s.label}</div>
            <div style={{ color: '#4E5953', fontSize: '0.75rem', marginTop: '2px' }}>{s.sub}</div>
          </div>
        ))}
      </div>

      {/* Status breakdown pie */}
      {!loading && kpis && pieData.length > 0 && (
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-5 mb-5">
          <div className="lg:col-span-1 bg-white rounded-xl p-5" style={{ border: '1px solid var(--border)' }}>
            <div className="mb-4">
              <h3 style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', fontSize: '1rem', marginBottom: '4px' }}>
                Status breakdown
              </h3>
              <p style={{ color: '#4E5953', fontSize: '0.8125rem' }}>By outcome for this period.</p>
            </div>
            <ResponsiveContainer width="100%" height={180}>
              <PieChart>
                <Pie data={pieData} cx="50%" cy="50%" innerRadius={45} outerRadius={75} paddingAngle={3} dataKey="value">
                  {pieData.map((entry, index) => (
                    <Cell key={entry.name} fill={PIE_COLORS[index % PIE_COLORS.length]} />
                  ))}
                </Pie>
                <Tooltip formatter={(value: number, name: string) => [value, name]} />
              </PieChart>
            </ResponsiveContainer>
            <div className="space-y-2 mt-2">
              {pieData.map((d, i) => (
                <div key={d.name} className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <div className="w-2.5 h-2.5 rounded-full" style={{ background: PIE_COLORS[i] }} />
                    <span style={{ color: '#4E5953', fontSize: '0.8125rem' }}>{d.name}</span>
                  </div>
                  <span style={{ color: '#1F2421', fontWeight: 600, fontSize: '0.8125rem' }}>{d.value}</span>
                </div>
              ))}
            </div>
          </div>

          {/* Summary card - approval vs rejection trend */}
          <div className="lg:col-span-2 bg-white rounded-xl p-5" style={{ border: '1px solid var(--border)' }}>
            <div className="mb-4">
              <h3 style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', fontSize: '1rem', marginBottom: '4px' }}>
                Approval vs rejection
              </h3>
              <p style={{ color: '#4E5953', fontSize: '0.8125rem' }}>
                Rate breakdown for the selected window ({kpis.window}).
              </p>
            </div>
            <ResponsiveContainer width="100%" height={200}>
              <AreaChart
                data={[
                  { name: 'Approved', value: kpis.approved },
                  { name: 'Rejected', value: kpis.rejected },
                  { name: 'Active', value: kpis.active },
                  { name: 'Completed', value: kpis.completed },
                  { name: 'Cancelled', value: kpis.cancelled },
                  { name: 'Expired', value: kpis.expired },
                ]}
                margin={{ top: 4, right: 4, left: -20, bottom: 0 }}
              >
                <defs>
                  <linearGradient id="areaGrad" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#2F6B55" stopOpacity={0.15} />
                    <stop offset="95%" stopColor="#2F6B55" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="#F0EDE7" />
                <XAxis dataKey="name" tick={{ fontSize: 11, fill: '#4E5953' }} axisLine={false} tickLine={false} />
                <YAxis tick={{ fontSize: 11, fill: '#4E5953' }} axisLine={false} tickLine={false} />
                <Tooltip content={<CustomTooltip />} />
                <Area type="monotone" dataKey="value" name="Count" stroke="#2F6B55" strokeWidth={2} fill="url(#areaGrad)" />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </div>
      )}

      {/* Empty state */}
      {!loading && !kpis && !error && (
        <div className="bg-white rounded-xl p-10 text-center" style={{ border: '1px solid var(--border)' }}>
          <p style={{ color: '#4E5953', fontSize: '0.9375rem' }}>No data available for this window.</p>
        </div>
      )}
    </div>
  );
}
