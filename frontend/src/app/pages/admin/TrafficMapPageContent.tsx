import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  TrendingUp, TrendingDown, Minus, RefreshCw, AlertTriangle, Info,
} from 'lucide-react';
import { getTrafficData, TrafficData, TrafficGraphNode } from '../../services/mapApi';

type LevelFilter = 'all' | 'low' | 'medium' | 'high' | 'critical';
type RegionFilter = 'all' | string;

const LEVEL_COLOR: Record<string, string> = {
  low: '#7FB069',
  medium: '#D9A441',
  high: '#C65D3A',
  critical: '#A61E1E',
};

const LEVEL_LABEL: Record<string, string> = {
  low: 'Low',
  medium: 'Moderate',
  high: 'High',
  critical: 'Critical',
};

const LEVEL_BG: Record<string, string> = {
  low: '#E8F4ED',
  medium: '#FFF4E0',
  high: '#FAEEE7',
  critical: '#FDECEA',
};

const SVG_W = 600;
const SVG_H = 460;

function titleCase(value: string): string {
  return value ? value.charAt(0).toUpperCase() + value.slice(1) : value;
}

function trendIcon(trend: string) {
  if (trend === 'improving') return <TrendingDown size={14} color="#2E7D32" />;
  if (trend === 'worsening') return <TrendingUp size={14} color="#B42318" />;
  return <Minus size={14} color="#4E5953" />;
}

function getNodePos(nodes: TrafficGraphNode[], nodeId: string) {
  const node = nodes.find((item) => item.node_id === nodeId);
  return node ? { x: node.x, y: node.y } : { x: 0, y: 0 };
}

export default function TrafficMapPageContent() {
  const [data, setData] = useState<TrafficData | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [regionFilter, setRegionFilter] = useState<RegionFilter>('all');
  const [levelFilter, setLevelFilter] = useState<LevelFilter>('all');
  const [lastRefresh, setLastRefresh] = useState<Date | null>(null);

  const loadTraffic = useCallback(async (showLoading = false) => {
    if (showLoading) {
      setLoading(true);
    } else {
      setRefreshing(true);
    }

    try {
      setError(null);
      const next = await getTrafficData();
      setData(next);
      setLastRefresh(new Date());
      setSelectedId((prev) => (next.segments.some((seg) => seg.segment_id === prev) ? prev : null));
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load traffic data');
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  }, []);

  useEffect(() => {
    void loadTraffic(true);
    const timer = window.setInterval(() => {
      void loadTraffic();
    }, 60_000);
    return () => window.clearInterval(timer);
  }, [loadTraffic]);

  const regionOptions = useMemo(() => {
    const values = Array.from(new Set((data?.segments ?? []).map((seg) => seg.region))).sort();
    return ['all', ...values];
  }, [data]);

  const filtered = useMemo(() => {
    const segments = data?.segments ?? [];
    return segments.filter((segment) => {
      const regionMatch = regionFilter === 'all' || segment.region === regionFilter;
      const levelMatch = levelFilter === 'all' || segment.level === levelFilter;
      return regionMatch && levelMatch;
    });
  }, [data, levelFilter, regionFilter]);

  const selected = useMemo(
    () => filtered.find((segment) => segment.segment_id === selectedId)
      ?? data?.segments.find((segment) => segment.segment_id === selectedId)
      ?? null,
    [data, filtered, selectedId],
  );

  const criticalCount = (data?.segments ?? []).filter((seg) => seg.level === 'critical').length;
  const highCount = (data?.segments ?? []).filter((seg) => seg.level === 'high').length;

  if (loading) {
    return <div className="p-8" style={{ color: '#4E5953' }}>Loading live traffic data...</div>;
  }

  if (error || !data) {
    return (
      <div className="p-5 lg:p-8 max-w-4xl mx-auto">
        <div className="rounded-2xl p-6" style={{ background: '#FDECEA', border: '1px solid #F5C2BE' }}>
          <div className="flex items-center gap-3 mb-3">
            <AlertTriangle size={18} color="#B42318" />
            <h1 style={{ fontFamily: 'var(--font-heading)', fontWeight: 700, color: '#8E1B13' }}>Traffic API unavailable</h1>
          </div>
          <p style={{ color: '#8E1B13', marginBottom: '16px' }}>{error ?? 'Failed to load traffic data.'}</p>
          <button
            onClick={() => {
              void loadTraffic(true);
            }}
            className="px-4 py-2 rounded-lg text-white text-sm"
            style={{ background: '#B42318', fontWeight: 600 }}
          >
            Retry
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="p-5 lg:p-8 max-w-7xl mx-auto">
      <div className="flex items-start justify-between gap-3 mb-5 flex-wrap">
        <div>
          <h1 style={{ fontFamily: 'var(--font-heading)', fontWeight: 700, color: '#1F2421', marginBottom: '4px' }}>
            Live traffic map
          </h1>
          <p style={{ color: '#4E5953', fontSize: '0.9375rem' }}>
            Live road occupancy and congestion trends from Map Service and Capacity Service.
          </p>
        </div>
        <div className="flex items-center gap-2 text-sm" style={{ color: '#4E5953' }}>
          <button
            type="button"
            onClick={() => void loadTraffic()}
            disabled={refreshing}
            className="flex items-center"
            style={{ color: '#4E5953', cursor: refreshing ? 'not-allowed' : 'pointer' }}
            aria-label="Refresh live traffic data"
          >
            <RefreshCw size={13} className={refreshing ? 'animate-spin' : ''} />
          </button>
          <span>Updated {lastRefresh?.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' })}</span>
        </div>
      </div>

      {(criticalCount > 0 || highCount > 0) && (
        <div className="flex items-center gap-3 p-3.5 rounded-xl mb-5 flex-wrap" style={{ background: '#FDECEA', border: '1px solid #F5C2BE' }}>
          <AlertTriangle size={16} color="#B42318" className="flex-shrink-0" />
          <p style={{ color: '#8E1B13', fontSize: '0.875rem', flex: 1 }}>
            {criticalCount > 0 && <><strong>{criticalCount} critical segment{criticalCount > 1 ? 's' : ''}</strong> at full capacity. </>}
            {highCount > 0 && <><strong>{highCount} segment{highCount > 1 ? 's' : ''}</strong> at high load.</>}
          </p>
        </div>
      )}

      <div className="flex gap-3 mb-5 flex-wrap">
        <select
          value={regionFilter}
          onChange={(e) => setRegionFilter(e.target.value)}
          className="px-3 py-2 rounded-lg outline-none appearance-none cursor-pointer text-sm"
          style={{ border: '1.5px solid var(--border)', color: '#1F2421', background: 'white' }}
        >
          {regionOptions.map((opt) => <option key={opt} value={opt}>{opt === 'all' ? 'All regions' : titleCase(opt)}</option>)}
        </select>

        <div className="flex rounded-lg overflow-hidden" style={{ border: '1.5px solid var(--border)' }}>
          {(['all', 'low', 'medium', 'high', 'critical'] as const).map((opt) => (
            <button
              key={opt}
              onClick={() => setLevelFilter(opt)}
              className="px-3 py-2 text-sm transition-colors"
              style={{
                background: levelFilter === opt ? '#1F2421' : 'white',
                color: levelFilter === opt ? 'white' : '#4E5953',
                fontWeight: levelFilter === opt ? 600 : 400,
                borderRight: opt !== 'critical' ? '1px solid var(--border)' : 'none',
              }}
            >
              {opt !== 'all' && <span className="inline-block w-2 h-2 rounded-full mr-1.5" style={{ background: LEVEL_COLOR[opt] }} />}
              {opt === 'all' ? 'All' : LEVEL_LABEL[opt]}
            </button>
          ))}
        </div>
      </div>

      <div className="flex flex-col lg:flex-row gap-5">
        <div className="flex-1 bg-white rounded-2xl overflow-hidden" style={{ border: '1px solid var(--border)' }}>
          <div className="px-5 py-3.5 flex items-center justify-between" style={{ borderBottom: '1px solid var(--border)' }}>
            <span style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem' }}>City road network</span>
            <span style={{ color: '#4E5953', fontSize: '0.8125rem' }}>{filtered.length} of {data.segments.length} segments shown</span>
          </div>

          <svg viewBox={`0 0 ${SVG_W} ${SVG_H}`} className="w-full" style={{ display: 'block', background: '#F8F6F2' }}>
            <rect width="100%" height="100%" fill="#F8F6F2" />
            {filtered.map((segment) => {
              const from = getNodePos(data.nodes, segment.from_node.node_id);
              const to = getNodePos(data.nodes, segment.to_node.node_id);
              const isSelected = selected?.segment_id === segment.segment_id;
              const color = LEVEL_COLOR[segment.level];
              return (
                <g key={segment.segment_id}>
                  {isSelected && <line x1={from.x} y1={from.y} x2={to.x} y2={to.y} stroke={color} strokeWidth="14" strokeLinecap="round" opacity={0.25} />}
                  <line
                    x1={from.x}
                    y1={from.y}
                    x2={to.x}
                    y2={to.y}
                    stroke={color}
                    strokeWidth={isSelected ? 7 : 5}
                    strokeLinecap="round"
                    style={{ cursor: 'pointer' }}
                    onClick={() => setSelectedId(segment.segment_id)}
                  />
                </g>
              );
            })}
            {data.nodes.map((node) => (
              <g key={node.node_id}>
                <circle cx={node.x} cy={node.y} r="10" fill="white" stroke="#D9D2C7" strokeWidth="1.5" />
                <circle cx={node.x} cy={node.y} r="4" fill="#4E5953" />
                <text x={node.x} y={node.y + 22} textAnchor="middle" style={{ fontSize: '10px', fill: '#1F2421', fontFamily: 'var(--font-body)', fontWeight: 600 }}>
                  {node.label}
                </text>
              </g>
            ))}
          </svg>
        </div>

        <div className="lg:w-80 flex-shrink-0 space-y-3">
          {selected && (
            <div className="bg-white rounded-2xl overflow-hidden" style={{ border: `2px solid ${LEVEL_COLOR[selected.level]}` }}>
              <div className="px-4 py-3" style={{ background: LEVEL_BG[selected.level], borderBottom: '1px solid var(--border)' }}>
                <div style={{ fontFamily: 'var(--font-heading)', fontWeight: 700, color: '#1F2421', fontSize: '0.9375rem' }}>{selected.name}</div>
                <div style={{ color: '#4E5953', fontSize: '0.8125rem' }}>{titleCase(selected.region)} region</div>
              </div>
              <div className="p-4 space-y-4">
                <div className="flex items-center justify-between">
                  <span style={{ color: '#4E5953', fontSize: '0.875rem' }}>Traffic level</span>
                  <span className="px-3 py-1 rounded-full text-sm" style={{ background: LEVEL_BG[selected.level], color: LEVEL_COLOR[selected.level], fontWeight: 600 }}>
                    {LEVEL_LABEL[selected.level]}
                  </span>
                </div>
                <div>
                  <div className="flex items-center justify-between mb-1.5">
                    <span style={{ color: '#4E5953', fontSize: '0.875rem' }}>Occupancy</span>
                    <span style={{ fontWeight: 700, color: '#1F2421', fontFamily: 'var(--font-heading)' }}>{selected.occupancy_pct}%</span>
                  </div>
                  <div className="w-full h-3 rounded-full overflow-hidden" style={{ background: '#F0EDE7' }}>
                    <div className="h-full rounded-full" style={{ width: `${selected.occupancy_pct}%`, background: LEVEL_COLOR[selected.level] }} />
                  </div>
                </div>
                <div className="grid grid-cols-2 gap-3">
                  <div className="p-3 rounded-lg" style={{ background: '#F8F6F2' }}>
                    <div style={{ color: '#4E5953', fontSize: '0.75rem' }}>Vehicles</div>
                    <div style={{ fontWeight: 700, color: '#1F2421', fontFamily: 'var(--font-heading)' }}>{selected.vehicles}</div>
                  </div>
                  <div className="p-3 rounded-lg" style={{ background: '#F8F6F2' }}>
                    <div style={{ color: '#4E5953', fontSize: '0.75rem' }}>Capacity</div>
                    <div style={{ fontWeight: 700, color: '#1F2421', fontFamily: 'var(--font-heading)' }}>{selected.capacity}</div>
                  </div>
                </div>
                <div className="flex items-center justify-between">
                  <span style={{ color: '#4E5953', fontSize: '0.875rem' }}>Trend</span>
                  <div className="flex items-center gap-1.5">
                    {trendIcon(selected.trend)}
                    <span style={{ color: '#4E5953', fontSize: '0.875rem', fontWeight: 500 }}>{titleCase(selected.trend)}</span>
                  </div>
                </div>
              </div>
            </div>
          )}

          <div className="bg-white rounded-2xl overflow-hidden" style={{ border: '1px solid var(--border)' }}>
            <div className="px-4 py-3.5" style={{ borderBottom: '1px solid var(--border)' }}>
              <span style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem' }}>Segment list</span>
            </div>
            <div className="divide-y" style={{ borderColor: 'var(--border)' }}>
              {filtered.length === 0 ? (
                <div className="p-5 text-center" style={{ color: '#4E5953', fontSize: '0.875rem' }}>No segments match filters.</div>
              ) : (
                filtered.map((segment) => (
                  <button
                    key={segment.segment_id}
                    onClick={() => setSelectedId(segment.segment_id)}
                    className="w-full flex items-center gap-3 px-4 py-3 text-left hover:bg-muted transition-colors"
                    style={{ background: selected?.segment_id === segment.segment_id ? '#F8F6F2' : 'transparent' }}
                  >
                    <div className="w-2.5 h-2.5 rounded-full flex-shrink-0" style={{ background: LEVEL_COLOR[segment.level] }} />
                    <div className="flex-1 min-w-0">
                      <div style={{ color: '#1F2421', fontWeight: 500, fontSize: '0.875rem' }} className="truncate">{segment.name}</div>
                      <div style={{ color: '#4E5953', fontSize: '0.75rem' }}>{titleCase(segment.region)} {segment.occupancy_pct}%</div>
                    </div>
                    {trendIcon(segment.trend)}
                  </button>
                ))
              )}
            </div>
          </div>

          <div className="flex items-start gap-2.5 p-3.5 rounded-xl" style={{ background: '#F0EDE7' }}>
            <Info size={14} color="#4E5953" className="flex-shrink-0 mt-0.5" />
            <p style={{ color: '#4E5953', fontSize: '0.8125rem', lineHeight: 1.55 }}>
              Segment data refreshes every 60 seconds. If the live API fails, this page now shows the error instead of mock traffic.
            </p>
          </div>
        </div>
      </div>
    </div>
  );
}
