import { useState, useCallback } from 'react';
import { mapNodes, trafficSegments, TrafficSegment, Region } from '../../data/mockData';
import {
  TrendingUp, TrendingDown, Minus, X, RefreshCw, AlertTriangle, Info,
} from 'lucide-react';

type LevelFilter = 'all' | 'low' | 'medium' | 'high' | 'critical';
type RegionFilter = Region | 'all';

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

const TREND_ICON = (trend: string) => {
  if (trend === 'improving') return <TrendingDown size={14} color="#2E7D32" />;
  if (trend === 'worsening') return <TrendingUp size={14} color="#B42318" />;
  return <Minus size={14} color="#4E5953" />;
};

const TREND_LABEL: Record<string, string> = {
  improving: 'Improving',
  stable: 'Stable',
  worsening: 'Worsening',
};

const REGION_OPTS: { label: string; value: RegionFilter }[] = [
  { label: 'All regions', value: 'all' },
  { label: 'North', value: 'North' },
  { label: 'South', value: 'South' },
  { label: 'East', value: 'East' },
  { label: 'West', value: 'West' },
  { label: 'Central', value: 'Central' },
];

const LEVEL_OPTS: { label: string; value: LevelFilter }[] = [
  { label: 'All', value: 'all' },
  { label: 'Low', value: 'low' },
  { label: 'Moderate', value: 'medium' },
  { label: 'High', value: 'high' },
  { label: 'Critical', value: 'critical' },
];

const SVG_W = 600;
const SVG_H = 460;

function getNodePos(nodeId: string) {
  const n = mapNodes.find((m) => m.id === nodeId);
  return n ? { x: n.x, y: n.y } : { x: 0, y: 0 };
}

export default function TrafficMapPage() {
  const [regionFilter, setRegionFilter] = useState<RegionFilter>('all');
  const [levelFilter, setLevelFilter] = useState<LevelFilter>('all');
  const [selected, setSelected] = useState<TrafficSegment | null>(null);
  const [lastRefresh] = useState(() => new Date());

  const filtered = trafficSegments.filter((s) => {
    const matchRegion = regionFilter === 'all' || s.region === regionFilter;
    const matchLevel = levelFilter === 'all' || s.level === levelFilter;
    return matchRegion && matchLevel;
  });

  const visibleIds = new Set(filtered.map((s) => s.id));

  const criticalCount = trafficSegments.filter((s) => s.level === 'critical').length;
  const highCount = trafficSegments.filter((s) => s.level === 'high').length;

  const handleSegmentClick = useCallback((seg: TrafficSegment) => {
    setSelected((prev) => (prev?.id === seg.id ? null : seg));
  }, []);

  return (
    <div className="p-5 lg:p-8 max-w-7xl mx-auto">
      {/* Header */}
      <div className="flex items-start justify-between gap-3 mb-5 flex-wrap">
        <div>
          <h1 style={{ fontFamily: 'var(--font-heading)', fontWeight: 700, color: '#1F2421', marginBottom: '4px' }}>
            Live traffic map
          </h1>
          <p style={{ color: '#4E5953', fontSize: '0.9375rem' }}>
            Road segment occupancy and congestion trends in real time.
          </p>
        </div>
        <div className="flex items-center gap-2 text-sm" style={{ color: '#4E5953' }}>
          <RefreshCw size={13} />
          <span>Updated {lastRefresh.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' })}</span>
        </div>
      </div>

      {/* Alert bar */}
      {(criticalCount > 0 || highCount > 0) && (
        <div
          className="flex items-center gap-3 p-3.5 rounded-xl mb-5 flex-wrap"
          style={{ background: '#FDECEA', border: '1px solid #F5C2BE' }}
        >
          <AlertTriangle size={16} color="#B42318" className="flex-shrink-0" />
          <p style={{ color: '#8E1B13', fontSize: '0.875rem', flex: 1 }}>
            {criticalCount > 0 && <><strong>{criticalCount} critical segment{criticalCount > 1 ? 's' : ''}</strong> at full capacity. </>}
            {highCount > 0 && <><strong>{highCount} segment{highCount > 1 ? 's' : ''}</strong> at high load. </>}
            New bookings on affected routes may be rejected.
          </p>
        </div>
      )}

      {/* Filters */}
      <div className="flex gap-3 mb-5 flex-wrap">
        <select
          value={regionFilter}
          onChange={(e) => setRegionFilter(e.target.value as RegionFilter)}
          className="px-3 py-2 rounded-lg outline-none appearance-none cursor-pointer text-sm"
          style={{ border: '1.5px solid var(--border)', color: '#1F2421', background: 'white' }}
        >
          {REGION_OPTS.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
        </select>

        <div className="flex rounded-lg overflow-hidden" style={{ border: '1.5px solid var(--border)' }}>
          {LEVEL_OPTS.map((o) => (
            <button
              key={o.value}
              onClick={() => setLevelFilter(o.value)}
              className="px-3 py-2 text-sm transition-colors"
              style={{
                background: levelFilter === o.value ? '#1F2421' : 'white',
                color: levelFilter === o.value ? 'white' : '#4E5953',
                fontWeight: levelFilter === o.value ? 600 : 400,
                borderRight: o.value !== 'critical' ? '1px solid var(--border)' : 'none',
              }}
            >
              {o.value !== 'all' && (
                <span
                  className="inline-block w-2 h-2 rounded-full mr-1.5"
                  style={{ background: LEVEL_COLOR[o.value] }}
                />
              )}
              {o.label}
            </button>
          ))}
        </div>
      </div>

      {/* Map + Detail panel */}
      <div className="flex flex-col lg:flex-row gap-5">
        {/* SVG Map */}
        <div className="flex-1 bg-white rounded-2xl overflow-hidden" style={{ border: '1px solid var(--border)' }}>
          {/* Map header */}
          <div className="px-5 py-3.5 flex items-center justify-between" style={{ borderBottom: '1px solid var(--border)' }}>
            <span style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem' }}>
              City road network
            </span>
            <span style={{ color: '#4E5953', fontSize: '0.8125rem' }}>
              {filtered.length} of {trafficSegments.length} segments shown
            </span>
          </div>

          <div className="relative">
            <svg
              viewBox={`0 0 ${SVG_W} ${SVG_H}`}
              className="w-full"
              style={{ display: 'block', background: '#F8F6F2' }}
              aria-label="City road traffic map"
            >
              {/* Background grid */}
              <defs>
                <pattern id="grid" width="40" height="40" patternUnits="userSpaceOnUse">
                  <path d="M 40 0 L 0 0 0 40" fill="none" stroke="#E8E4DE" strokeWidth="0.5" />
                </pattern>
              </defs>
              <rect width="100%" height="100%" fill="url(#grid)" />

              {/* City blocks hint */}
              {[
                [160, 130, 120, 90], [310, 130, 120, 90],
                [160, 280, 120, 90], [310, 280, 120, 90],
              ].map(([x, y, w, h], i) => (
                <rect key={i} x={x} y={y} width={w} height={h} rx="4" fill="#EDE9E2" stroke="#D9D2C7" strokeWidth="0.5" />
              ))}

              {/* Road segments */}
              {trafficSegments.map((seg) => {
                const from = getNodePos(seg.fromNode);
                const to = getNodePos(seg.toNode);
                const isVisible = visibleIds.has(seg.id);
                const isSelected = selected?.id === seg.id;
                const color = isVisible ? LEVEL_COLOR[seg.level] : '#E0DAD3';

                return (
                  <g key={seg.id}>
                    {/* Shadow/glow for selected */}
                    {isSelected && (
                      <line
                        x1={from.x} y1={from.y}
                        x2={to.x} y2={to.y}
                        stroke={color}
                        strokeWidth="14"
                        strokeLinecap="round"
                        opacity={0.25}
                      />
                    )}
                    {/* Main segment line */}
                    <line
                      x1={from.x} y1={from.y}
                      x2={to.x} y2={to.y}
                      stroke={color}
                      strokeWidth={isSelected ? 7 : 5}
                      strokeLinecap="round"
                      opacity={isVisible ? 1 : 0.25}
                      style={{ cursor: 'pointer', transition: 'stroke-width 0.15s, opacity 0.15s' }}
                      onClick={() => isVisible && handleSegmentClick(seg)}
                      aria-label={`${seg.name}: ${LEVEL_LABEL[seg.level]} traffic`}
                      role="button"
                      tabIndex={isVisible ? 0 : -1}
                      onKeyDown={(e) => e.key === 'Enter' && isVisible && handleSegmentClick(seg)}
                    />
                    {/* Direction arrow midpoint indicator */}
                    {isVisible && (
                      <circle
                        cx={(from.x + to.x) / 2}
                        cy={(from.y + to.y) / 2}
                        r="4"
                        fill="white"
                        stroke={color}
                        strokeWidth="2"
                        style={{ pointerEvents: 'none' }}
                      />
                    )}
                  </g>
                );
              })}

              {/* Nodes */}
              {mapNodes.map((node) => (
                <g key={node.id}>
                  <circle cx={node.x} cy={node.y} r="10" fill="white" stroke="#D9D2C7" strokeWidth="1.5" />
                  <circle cx={node.x} cy={node.y} r="4" fill="#4E5953" />
                  {/* Label */}
                  <text
                    x={node.x}
                    y={node.y + 22}
                    textAnchor="middle"
                    style={{ fontSize: '10px', fill: '#1F2421', fontFamily: 'var(--font-body)', fontWeight: 600 }}
                  >
                    {node.label}
                  </text>
                </g>
              ))}
            </svg>
          </div>

          {/* Legend */}
          <div className="px-5 py-4 flex items-center gap-5 flex-wrap" style={{ borderTop: '1px solid var(--border)' }}>
            <span style={{ color: '#4E5953', fontSize: '0.8125rem', fontWeight: 500 }}>Traffic level:</span>
            {Object.entries(LEVEL_COLOR).map(([level, color]) => (
              <div key={level} className="flex items-center gap-1.5">
                <div className="w-6 h-1.5 rounded-full" style={{ background: color }} />
                <span style={{ color: '#4E5953', fontSize: '0.8125rem' }}>{LEVEL_LABEL[level]}</span>
              </div>
            ))}
            <div className="flex items-center gap-1.5">
              <div className="w-2 h-2 rounded-full bg-white" style={{ border: '2px solid #4E5953' }} />
              <span style={{ color: '#4E5953', fontSize: '0.8125rem' }}>Junction</span>
            </div>
            <div style={{ color: '#4E5953', fontSize: '0.8125rem' }}>
              <strong>Tip:</strong> Click a segment to see details.
            </div>
          </div>
        </div>

        {/* Detail Panel / Segment list */}
        <div className="lg:w-80 flex-shrink-0 space-y-3">
          {/* Selected segment detail */}
          {selected && (
            <div className="bg-white rounded-2xl overflow-hidden" style={{ border: `2px solid ${LEVEL_COLOR[selected.level]}` }}>
              <div
                className="flex items-center justify-between px-4 py-3"
                style={{ background: LEVEL_BG[selected.level], borderBottom: '1px solid var(--border)' }}
              >
                <div>
                  <div style={{ fontFamily: 'var(--font-heading)', fontWeight: 700, color: '#1F2421', fontSize: '0.9375rem' }}>
                    {selected.name}
                  </div>
                  <div style={{ color: '#4E5953', fontSize: '0.8125rem' }}>{selected.region} region</div>
                </div>
                <button
                  onClick={() => setSelected(null)}
                  className="p-1 rounded-lg hover:bg-black/5 transition-colors"
                  aria-label="Close detail"
                >
                  <X size={16} color="#4E5953" />
                </button>
              </div>

              <div className="p-4 space-y-4">
                {/* Level */}
                <div className="flex items-center justify-between">
                  <span style={{ color: '#4E5953', fontSize: '0.875rem' }}>Traffic level</span>
                  <span
                    className="px-3 py-1 rounded-full text-sm"
                    style={{ background: LEVEL_BG[selected.level], color: LEVEL_COLOR[selected.level], fontWeight: 600 }}
                  >
                    {LEVEL_LABEL[selected.level]}
                  </span>
                </div>

                {/* Occupancy bar */}
                <div>
                  <div className="flex items-center justify-between mb-1.5">
                    <span style={{ color: '#4E5953', fontSize: '0.875rem' }}>Occupancy</span>
                    <span style={{ fontWeight: 700, color: '#1F2421', fontFamily: 'var(--font-heading)' }}>
                      {selected.occupancy}%
                    </span>
                  </div>
                  <div className="w-full h-3 rounded-full overflow-hidden" style={{ background: '#F0EDE7' }}>
                    <div
                      className="h-full rounded-full transition-all duration-500"
                      style={{ width: `${selected.occupancy}%`, background: LEVEL_COLOR[selected.level] }}
                    />
                  </div>
                  <div className="flex justify-between mt-1">
                    <span style={{ color: '#4E5953', fontSize: '0.75rem' }}>0%</span>
                    <span style={{ color: '#4E5953', fontSize: '0.75rem' }}>100% capacity</span>
                  </div>
                </div>

                {/* Stats */}
                <div className="grid grid-cols-2 gap-3">
                  <div className="p-3 rounded-lg" style={{ background: '#F8F6F2' }}>
                    <div style={{ color: '#4E5953', fontSize: '0.75rem', marginBottom: '2px' }}>Vehicles</div>
                    <div style={{ fontWeight: 700, color: '#1F2421', fontFamily: 'var(--font-heading)' }}>{selected.vehicles}</div>
                  </div>
                  <div className="p-3 rounded-lg" style={{ background: '#F8F6F2' }}>
                    <div style={{ color: '#4E5953', fontSize: '0.75rem', marginBottom: '2px' }}>Capacity</div>
                    <div style={{ fontWeight: 700, color: '#1F2421', fontFamily: 'var(--font-heading)' }}>{selected.capacity}</div>
                  </div>
                </div>

                {/* Trend */}
                <div className="flex items-center justify-between">
                  <span style={{ color: '#4E5953', fontSize: '0.875rem' }}>Trend</span>
                  <div className="flex items-center gap-1.5">
                    {TREND_ICON(selected.trend)}
                    <span
                      style={{
                        color: selected.trend === 'improving' ? '#2E7D32' : selected.trend === 'worsening' ? '#B42318' : '#4E5953',
                        fontSize: '0.875rem',
                        fontWeight: 500,
                      }}
                    >
                      {TREND_LABEL[selected.trend]}
                    </span>
                  </div>
                </div>

                {selected.level === 'critical' && (
                  <div className="flex items-start gap-2 p-3 rounded-lg" style={{ background: '#FDECEA' }}>
                    <AlertTriangle size={14} color="#B42318" className="flex-shrink-0 mt-0.5" />
                    <p style={{ color: '#8E1B13', fontSize: '0.8125rem', lineHeight: 1.5 }}>
                      This segment is at capacity. New bookings routing through here will be rejected until congestion clears.
                    </p>
                  </div>
                )}
              </div>
            </div>
          )}

          {/* Segment list */}
          <div className="bg-white rounded-2xl overflow-hidden" style={{ border: '1px solid var(--border)' }}>
            <div className="px-4 py-3.5" style={{ borderBottom: '1px solid var(--border)' }}>
              <span style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem' }}>
                Segment list
              </span>
            </div>
            <div className="divide-y" style={{ borderColor: 'var(--border)' }}>
              {filtered.length === 0 ? (
                <div className="p-5 text-center">
                  <p style={{ color: '#4E5953', fontSize: '0.875rem' }}>No segments match filters.</p>
                </div>
              ) : (
                filtered.map((seg) => (
                  <button
                    key={seg.id}
                    onClick={() => handleSegmentClick(seg)}
                    className="w-full flex items-center gap-3 px-4 py-3 text-left hover:bg-muted transition-colors"
                    style={{ background: selected?.id === seg.id ? '#F8F6F2' : 'transparent' }}
                  >
                    <div
                      className="w-2.5 h-2.5 rounded-full flex-shrink-0"
                      style={{ background: LEVEL_COLOR[seg.level] }}
                    />
                    <div className="flex-1 min-w-0">
                      <div style={{ color: '#1F2421', fontWeight: 500, fontSize: '0.875rem' }} className="truncate">
                        {seg.name}
                      </div>
                      <div style={{ color: '#4E5953', fontSize: '0.75rem' }}>{seg.region} · {seg.occupancy}%</div>
                    </div>
                    <div className="flex items-center gap-1">
                      {TREND_ICON(seg.trend)}
                    </div>
                  </button>
                ))
              )}
            </div>
          </div>

          {/* Info note */}
          <div className="flex items-start gap-2.5 p-3.5 rounded-xl" style={{ background: '#F0EDE7' }}>
            <Info size={14} color="#4E5953" className="flex-shrink-0 mt-0.5" />
            <p style={{ color: '#4E5953', fontSize: '0.8125rem', lineHeight: 1.55 }}>
              Segment data updates every 60 seconds. Click any segment on the map or in the list to view detailed occupancy.
            </p>
          </div>
        </div>
      </div>
    </div>
  );
}
