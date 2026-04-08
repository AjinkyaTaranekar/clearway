import { Activity, CheckCircle2, Clock, TrendingDown, TrendingUp, XCircle } from 'lucide-react';
import { useEffect, useState } from 'react';
import {
  Area,
  AreaChart,
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis, YAxis
} from 'recharts';
import { analyticsData } from '../../data/mockData';
import { AdminAnalyticsResult, adminAnalytics } from '../../services/journeyApi';

type TimeWindow = '1h' | '24h' | '7d';

const TIME_WINDOWS: { label: string; value: TimeWindow }[] = [
  { label: 'Last hour', value: '1h' },
  { label: 'Last 24 hours', value: '24h' },
  { label: 'Last 7 days', value: '7d' },
];

// Mock trend data per time window
const trendData: Record<TimeWindow, { time: string; bookings: number; approved: number; rejected: number }[]> = {
  '1h': [
    { time: '13:00', bookings: 4, approved: 3, rejected: 1 },
    { time: '13:10', bookings: 6, approved: 5, rejected: 1 },
    { time: '13:20', bookings: 3, approved: 3, rejected: 0 },
    { time: '13:30', bookings: 8, approved: 7, rejected: 1 },
    { time: '13:40', bookings: 5, approved: 4, rejected: 1 },
    { time: '13:50', bookings: 7, approved: 6, rejected: 1 },
    { time: '14:00', bookings: 4, approved: 4, rejected: 0 },
  ],
  '24h': [
    { time: '00:00', bookings: 2, approved: 2, rejected: 0 },
    { time: '03:00', bookings: 1, approved: 1, rejected: 0 },
    { time: '06:00', bookings: 8, approved: 6, rejected: 2 },
    { time: '09:00', bookings: 22, approved: 16, rejected: 6 },
    { time: '12:00', bookings: 18, approved: 16, rejected: 2 },
    { time: '15:00', bookings: 25, approved: 22, rejected: 3 },
    { time: '18:00', bookings: 20, approved: 18, rejected: 2 },
    { time: '21:00', bookings: 12, approved: 11, rejected: 1 },
  ],
  '7d': analyticsData.bookingTrend,
};

const kpisByWindow: Record<TimeWindow, { totalBookings: number; approvalRate: number; rejectionRate: number; activeJourneys: number; cancellations: number; avgDuration: number }> = {
  '1h': { totalBookings: 37, approvalRate: 91.2, rejectionRate: 8.8, activeJourneys: 14, cancellations: 2, avgDuration: 38 },
  '24h': { totalBookings: 108, approvalRate: 88.0, rejectionRate: 12.0, activeJourneys: 14, cancellations: 9, avgDuration: 41 },
  '7d': analyticsData.kpis,
};

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
  const [window, setWindow] = useState<TimeWindow>('7d');
  const trend = trendData[window];

  // U-14: fetch live KPIs from backend; fall back to mock on error
  const [liveKpis, setLiveKpis] = useState<AdminAnalyticsResult | null>(null);
  useEffect(() => {
    adminAnalytics(window).then(setLiveKpis).catch(() => setLiveKpis(null));
  }, [window]);

  const kpis = liveKpis
    ? {
        totalBookings: liveKpis.total_journeys,
        approvalRate:  liveKpis.approval_rate,
        rejectionRate: liveKpis.rejection_rate,
        activeJourneys: liveKpis.active,
        cancellations:  liveKpis.cancelled,
        avgDuration:    0, // not yet tracked server-side
      }
    : kpisByWindow[window];

  const pieData = [
    { name: 'Approved', value: Math.round(kpis.totalBookings * kpis.approvalRate / 100) },
    { name: 'Rejected', value: Math.round(kpis.totalBookings * kpis.rejectionRate / 100) },
    { name: 'Cancelled', value: kpis.cancellations },
    { name: 'Active', value: kpis.activeJourneys },
  ];

  const statCards = [
    {
      label: 'Total bookings',
      value: kpis.totalBookings.toString(),
      icon: Activity,
      color: '#2F6B55',
      bg: '#E8F4ED',
      trend: '+12% vs previous',
      trendUp: true,
    },
    {
      label: 'Approval rate',
      value: `${kpis.approvalRate}%`,
      icon: TrendingUp,
      color: '#2E7D32',
      bg: '#E8F4ED',
      trend: '+2.3% vs previous',
      trendUp: true,
    },
    {
      label: 'Rejection rate',
      value: `${kpis.rejectionRate}%`,
      icon: TrendingDown,
      color: '#B42318',
      bg: '#FDECEA',
      trend: '-2.3% vs previous',
      trendUp: false,
    },
    {
      label: 'Active journeys',
      value: kpis.activeJourneys.toString(),
      icon: CheckCircle2,
      color: '#1A4E80',
      bg: '#E3EEFB',
      trend: 'Right now',
      trendUp: null,
    },
    {
      label: 'Cancellations',
      value: kpis.cancellations.toString(),
      icon: XCircle,
      color: '#B65C3A',
      bg: '#FAEEE7',
      trend: '+3 vs previous',
      trendUp: false,
    },
    {
      label: 'Avg. duration',
      value: `${kpis.avgDuration} min`,
      icon: Clock,
      color: '#4E5953',
      bg: '#F0EDE7',
      trend: 'Per journey',
      trendUp: null,
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

        {/* Time window selector */}
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

      {/* KPI Cards */}
      <div className="grid grid-cols-2 lg:grid-cols-3 gap-3 mb-6">
        {statCards.map((s) => (
          <div key={s.label} className="bg-white rounded-xl p-4" style={{ border: '1px solid var(--border)' }}>
            <div className="flex items-start justify-between mb-3">
              <div className="w-9 h-9 rounded-lg flex items-center justify-center" style={{ background: s.bg }}>
                <s.icon size={17} color={s.color} />
              </div>
              {s.trendUp !== null && (
                <div className="flex items-center gap-1" style={{ color: s.trendUp ? '#2E7D32' : '#B42318' }}>
                  {s.trendUp ? <TrendingUp size={13} /> : <TrendingDown size={13} />}
                </div>
              )}
            </div>
            <div style={{ fontFamily: 'var(--font-heading)', fontWeight: 700, fontSize: '1.625rem', color: '#1F2421', lineHeight: 1, marginBottom: '4px' }}>
              {s.value}
            </div>
            <div style={{ color: '#1F2421', fontSize: '0.8125rem', fontWeight: 500 }}>{s.label}</div>
            <div style={{ color: '#4E5953', fontSize: '0.75rem', marginTop: '2px' }}>{s.trend}</div>
          </div>
        ))}
      </div>

      {/* Charts grid */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-5 mb-5">
        {/* Booking trend - takes 2 columns */}
        <div className="lg:col-span-2 bg-white rounded-xl p-5" style={{ border: '1px solid var(--border)' }}>
          <div className="mb-4">
            <h3 style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', fontSize: '1rem', marginBottom: '4px' }}>
              Booking trend
            </h3>
            <p style={{ color: '#4E5953', fontSize: '0.8125rem' }}>Total bookings vs approved and rejected.</p>
          </div>
          <ResponsiveContainer width="100%" height={240}>
            <AreaChart data={trend} margin={{ top: 4, right: 4, left: -20, bottom: 0 }}>
              <defs>
                <linearGradient id="bookingGrad" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="5%" stopColor="#2F6B55" stopOpacity={0.15} />
                  <stop offset="95%" stopColor="#2F6B55" stopOpacity={0} />
                </linearGradient>
                <linearGradient id="approvedGrad" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="5%" stopColor="#7FB069" stopOpacity={0.15} />
                  <stop offset="95%" stopColor="#7FB069" stopOpacity={0} />
                </linearGradient>
              </defs>
              <CartesianGrid strokeDasharray="3 3" stroke="#F0EDE7" />
              <XAxis dataKey="time" tick={{ fontSize: 11, fill: '#4E5953' }} axisLine={false} tickLine={false} />
              <YAxis tick={{ fontSize: 11, fill: '#4E5953' }} axisLine={false} tickLine={false} />
              <Tooltip content={<CustomTooltip />} />
              <Area type="monotone" dataKey="bookings" name="Bookings" stroke="#2F6B55" strokeWidth={2} fill="url(#bookingGrad)" />
              <Area type="monotone" dataKey="approved" name="Approved" stroke="#7FB069" strokeWidth={2} fill="url(#approvedGrad)" />
              <Area type="monotone" dataKey="rejected" name="Rejected" stroke="#B42318" strokeWidth={2} fill="none" strokeDasharray="4 2" />
            </AreaChart>
          </ResponsiveContainer>
          <div className="flex items-center gap-5 mt-3 flex-wrap">
            {[
              { color: '#2F6B55', label: 'Total bookings' },
              { color: '#7FB069', label: 'Approved' },
              { color: '#B42318', label: 'Rejected', dashed: true },
            ].map((l) => (
              <div key={l.label} className="flex items-center gap-1.5">
                <div className="w-3 h-0.5 rounded" style={{ background: l.color, opacity: l.dashed ? 0.8 : 1 }} />
                <span style={{ color: '#4E5953', fontSize: '0.75rem' }}>{l.label}</span>
              </div>
            ))}
          </div>
        </div>

        {/* Status pie */}
        <div className="bg-white rounded-xl p-5" style={{ border: '1px solid var(--border)' }}>
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
      </div>

      {/* Regional breakdown */}
      <div className="bg-white rounded-xl p-5" style={{ border: '1px solid var(--border)' }}>
        <div className="mb-4">
          <h3 style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', fontSize: '1rem', marginBottom: '4px' }}>
            Regional breakdown
          </h3>
          <p style={{ color: '#4E5953', fontSize: '0.8125rem' }}>Bookings and approval rates by region.</p>
        </div>
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
          <ResponsiveContainer width="100%" height={220}>
            <BarChart data={analyticsData.regionalStats} margin={{ top: 4, right: 4, left: -20, bottom: 0 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="#F0EDE7" />
              <XAxis dataKey="region" tick={{ fontSize: 11, fill: '#4E5953' }} axisLine={false} tickLine={false} />
              <YAxis tick={{ fontSize: 11, fill: '#4E5953' }} axisLine={false} tickLine={false} />
              <Tooltip content={<CustomTooltip />} />
              <Bar dataKey="approved" name="Approved" fill="#2F6B55" radius={[4, 4, 0, 0]} />
              <Bar dataKey="rejected" name="Rejected" fill="#B42318" radius={[4, 4, 0, 0]} />
            </BarChart>
          </ResponsiveContainer>

          {/* Regional table */}
          <div className="overflow-hidden rounded-xl" style={{ border: '1px solid var(--border)' }}>
            <table className="w-full">
              <thead>
                <tr style={{ background: '#F8F6F2', borderBottom: '1px solid var(--border)' }}>
                  <th className="px-4 py-2.5 text-left" style={{ color: '#4E5953', fontSize: '0.75rem', fontWeight: 500, fontFamily: 'var(--font-body)' }}>Region</th>
                  <th className="px-4 py-2.5 text-right" style={{ color: '#4E5953', fontSize: '0.75rem', fontWeight: 500, fontFamily: 'var(--font-body)' }}>Bookings</th>
                  <th className="px-4 py-2.5 text-right" style={{ color: '#4E5953', fontSize: '0.75rem', fontWeight: 500, fontFamily: 'var(--font-body)' }}>Approval</th>
                </tr>
              </thead>
              <tbody>
                {analyticsData.regionalStats.map((r, i, arr) => {
                  const rate = ((r.approved / r.bookings) * 100).toFixed(0);
                  return (
                    <tr key={r.region} style={{ borderBottom: i < arr.length - 1 ? '1px solid var(--border)' : 'none' }}>
                      <td className="px-4 py-3">
                        <span style={{ color: '#1F2421', fontWeight: 500, fontSize: '0.875rem' }}>{r.region}</span>
                      </td>
                      <td className="px-4 py-3 text-right">
                        <span style={{ color: '#1F2421', fontSize: '0.875rem' }}>{r.bookings}</span>
                      </td>
                      <td className="px-4 py-3 text-right">
                        <span
                          style={{
                            color: parseInt(rate) >= 85 ? '#2E7D32' : parseInt(rate) >= 70 ? '#A15C00' : '#B42318',
                            fontWeight: 600,
                            fontSize: '0.875rem',
                          }}
                        >
                          {rate}%
                        </span>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </div>
      </div>

      {/* Insight callout */}
      <div className="mt-5 p-4 rounded-xl flex items-start gap-3" style={{ background: '#FFF4E0', border: '1px solid #F0D8A0' }}>
        <AlertCircle size={16} color="#A15C00" className="flex-shrink-0 mt-0.5" />
        <p style={{ color: '#7A4500', fontSize: '0.875rem', lineHeight: 1.6 }}>
          <strong style={{ color: '#1F2421' }}>East and West regions</strong> have the highest rejection rates. Consider adjusting capacity limits on the East Ring Road and Port Road segments during peak hours.
        </p>
      </div>
    </div>
  );
}
