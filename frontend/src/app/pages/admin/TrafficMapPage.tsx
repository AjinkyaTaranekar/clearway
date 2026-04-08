import tt from '@tomtom-international/web-sdk-maps';
import {
  AlertTriangle,
  Info,
  Minus,
  RefreshCw,
  TrendingDown,
  TrendingUp,
  X,
} from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import TomTomMap, { addMarker } from '../../components/ui/TomTomMap';
import { TrafficSegment, getTrafficData } from '../../services/mapApi';

type LevelFilter = 'all' | 'low' | 'medium' | 'high' | 'critical';

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

const LEVEL_OPTS: { label: string; value: LevelFilter }[] = [
  { label: 'All', value: 'all' },
  { label: 'Low', value: 'low' },
  { label: 'Moderate', value: 'medium' },
  { label: 'High', value: 'high' },
  { label: 'Critical', value: 'critical' },
];

// Dublin city centre default
const DUBLIN_CENTRE: [number, number] = [53.3498, -6.2603];

export default function TrafficMapPage() {
  const [levelFilter, setLevelFilter] = useState<LevelFilter>('all');
  const [selected, setSelected] = useState<TrafficSegment | null>(null);
  const [segments, setSegments] = useState<TrafficSegment[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [lastRefresh, setLastRefresh] = useState<Date>(new Date());

  const mapRef = useRef<tt.Map | null>(null);
  // Track drawn segment layer IDs so we can update them on refresh
  const drawnLayersRef = useRef<Set<string>>(new Set());

  const fetchTraffic = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await getTrafficData();
      setSegments(data.segments);
      setLastRefresh(new Date());
    } catch {
      setError('Unable to load traffic data. Retrying in 60 s.');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchTraffic();
    const timer = setInterval(fetchTraffic, 60_000);
    return () => clearInterval(timer);
  }, [fetchTraffic]);

  // Draw/update segment overlays whenever segments or filter changes
  useEffect(() => {
    const map = mapRef.current;
    if (!map || segments.length === 0) return;
    drawSegments(map, segments, levelFilter, selected?.segment_id ?? null);
  }, [segments, levelFilter, selected]);

  const drawSegments = (
    map: tt.Map,
    segs: TrafficSegment[],
    filter: LevelFilter,
    selectedId: string | null,
  ) => {
    segs.forEach((seg) => {
      const sourceId = `seg-${seg.segment_id}`;
      const layerId = `seg-layer-${seg.segment_id}`;
      const isVisible = filter === 'all' || seg.level === filter;
      const isSelected = seg.segment_id === selectedId;
      const color = LEVEL_COLOR[seg.level] ?? '#4E5953';
      const width = isSelected ? 8 : 5;
      const opacity = isVisible ? 1 : 0.2;

      const from = seg.from_node;
      const to = seg.to_node;
      if (!from || !to) return;

      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const geojson: any = {
        type: 'Feature',
        geometry: {
          type: 'LineString',
          coordinates: [
            [from.lng, from.lat],
            [to.lng, to.lat],
          ],
        },
        properties: {},
      };

      if (map.getSource(sourceId)) {
        (map.getSource(sourceId) as tt.GeoJSONSource).setData(geojson);
        map.setPaintProperty(layerId, 'line-color', color);
        map.setPaintProperty(layerId, 'line-width', width);
        map.setPaintProperty(layerId, 'line-opacity', opacity);
      } else {
        map.addSource(sourceId, { type: 'geojson', data: geojson });
        map.addLayer({
          id: layerId,
          type: 'line',
          source: sourceId,
          layout: { 'line-join': 'round', 'line-cap': 'round' },
          paint: { 'line-color': color, 'line-width': width, 'line-opacity': opacity },
        });

        // Click to select segment
        map.on('click', layerId, () => {
          setSelected((prev) => (prev?.segment_id === seg.segment_id ? null : seg));
        });
        map.on('mouseenter', layerId, () => { map.getCanvas().style.cursor = 'pointer'; });
        map.on('mouseleave', layerId, () => { map.getCanvas().style.cursor = ''; });
        drawnLayersRef.current.add(layerId);
      }
    });
  };

  const handleMapReady = (map: tt.Map) => {
    mapRef.current = map;
    if (segments.length > 0) {
      drawSegments(map, segments, levelFilter, selected?.segment_id ?? null);
    }
  };

  const filtered = segments.filter(
    (s) => levelFilter === 'all' || s.level === levelFilter,
  );

  const criticalCount = segments.filter((s) => s.level === 'critical').length;
  const highCount = segments.filter((s) => s.level === 'high').length;

  // Add node markers after map ready
  useEffect(() => {
    const map = mapRef.current;
    if (!map || segments.length === 0) return;
    // Add unique node markers
    const seen = new Set<string>();
    segments.forEach((seg) => {
      if (seg.from_node && !seen.has(seg.from_node.node_id)) {
        seen.add(seg.from_node.node_id);
        addMarker(map, seg.from_node.lat, seg.from_node.lng, '#4E5953');
      }
      if (seg.to_node && !seen.has(seg.to_node.node_id)) {
        seen.add(seg.to_node.node_id);
        addMarker(map, seg.to_node.lat, seg.to_node.lng, '#4E5953');
      }
    });
  }, [segments]);

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
        <button
          onClick={fetchTraffic}
          disabled={loading}
          className="flex items-center gap-2 text-sm"
          style={{ color: '#4E5953' }}
        >
          <RefreshCw size={13} className={loading ? 'animate-spin' : ''} />
          <span>
            {loading ? 'Refreshing…' : `Updated ${lastRefresh.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' })}`}
          </span>
        </button>
      </div>

      {/* Alert bar */}
      {(criticalCount > 0 || highCount > 0) && (
        <div
          className="flex items-center gap-3 p-3.5 rounded-xl mb-5 flex-wrap"
          style={{ background: '#FDECEA', border: '1px solid #F5C2BE' }}
        >
          <AlertTriangle size={16} color="#B42318" className="flex-shrink-0" />
          <p style={{ color: '#8E1B13', fontSize: '0.875rem', flex: 1 }}>
            {criticalCount > 0 && (
              <><strong>{criticalCount} critical segment{criticalCount > 1 ? 's' : ''}</strong> at full capacity. </>
            )}
            {highCount > 0 && (
              <><strong>{highCount} segment{highCount > 1 ? 's' : ''}</strong> at high load. </>
            )}
            New bookings on affected routes may be rejected.
          </p>
        </div>
      )}

      {error && (
        <div
          className="flex items-center gap-3 p-3.5 rounded-xl mb-5"
          style={{ background: '#FEF3F2', border: '1px solid #FECACA' }}
        >
          <AlertTriangle size={16} color="#B42318" />
          <p style={{ color: '#B42318', fontSize: '0.875rem' }}>{error}</p>
        </div>
      )}

      {/* Level filter */}
      <div className="flex gap-3 mb-5 flex-wrap">
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
                <span className="inline-block w-2 h-2 rounded-full mr-1.5" style={{ background: LEVEL_COLOR[o.value] }} />
              )}
              {o.label}
            </button>
          ))}
        </div>
        <span style={{ color: '#4E5953', fontSize: '0.875rem', alignSelf: 'center' }}>
          {filtered.length} of {segments.length} segments shown
        </span>
      </div>

      {/* Map + sidebar */}
      <div className="flex flex-col lg:flex-row gap-5">
        {/* TomTom map */}
        <div className="flex-1 bg-white rounded-2xl overflow-hidden" style={{ border: '1px solid var(--border)', minHeight: '480px' }}>
          <div className="px-5 py-3.5 flex items-center justify-between" style={{ borderBottom: '1px solid var(--border)' }}>
            <span style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem' }}>
              Live road network
            </span>
            <span style={{ color: '#4E5953', fontSize: '0.8125rem' }}>Click a segment for details</span>
          </div>

          <TomTomMap
            center={DUBLIN_CENTRE}
            zoom={12}
            onReady={handleMapReady}
            style={{ height: '420px' }}
          />

          {/* Legend */}
          <div className="px-5 py-4 flex items-center gap-5 flex-wrap" style={{ borderTop: '1px solid var(--border)' }}>
            <span style={{ color: '#4E5953', fontSize: '0.8125rem', fontWeight: 500 }}>Traffic level:</span>
            {Object.entries(LEVEL_COLOR).map(([level, color]) => (
              <div key={level} className="flex items-center gap-1.5">
                <div className="w-6 h-1.5 rounded-full" style={{ background: color }} />
                <span style={{ color: '#4E5953', fontSize: '0.8125rem' }}>{LEVEL_LABEL[level]}</span>
              </div>
            ))}
          </div>
        </div>

        {/* Sidebar */}
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
                <button onClick={() => setSelected(null)} className="p-1 rounded-lg hover:bg-black/5 transition-colors" aria-label="Close">
                  <X size={16} color="#4E5953" />
                </button>
              </div>

              <div className="p-4 space-y-4">
                <div className="flex items-center justify-between">
                  <span style={{ color: '#4E5953', fontSize: '0.875rem' }}>Traffic level</span>
                  <span
                    className="px-3 py-1 rounded-full text-sm"
                    style={{ background: LEVEL_BG[selected.level], color: LEVEL_COLOR[selected.level], fontWeight: 600 }}
                  >
                    {LEVEL_LABEL[selected.level]}
                  </span>
                </div>

                <div>
                  <div className="flex items-center justify-between mb-1.5">
                    <span style={{ color: '#4E5953', fontSize: '0.875rem' }}>Occupancy</span>
                    <span style={{ fontWeight: 700, color: '#1F2421', fontFamily: 'var(--font-heading)' }}>
                      {selected.occupancy_pct}%
                    </span>
                  </div>
                  <div className="w-full h-3 rounded-full overflow-hidden" style={{ background: '#F0EDE7' }}>
                    <div
                      className="h-full rounded-full transition-all duration-500"
                      style={{ width: `${selected.occupancy_pct}%`, background: LEVEL_COLOR[selected.level] }}
                    />
                  </div>
                  <div className="flex justify-between mt-1">
                    <span style={{ color: '#4E5953', fontSize: '0.75rem' }}>0%</span>
                    <span style={{ color: '#4E5953', fontSize: '0.75rem' }}>100% capacity</span>
                  </div>
                </div>

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
            <div className="divide-y" style={{ borderColor: 'var(--border)', maxHeight: '340px', overflowY: 'auto' }}>
              {loading && segments.length === 0 ? (
                <div className="p-5 text-center">
                  <p style={{ color: '#4E5953', fontSize: '0.875rem' }}>Loading segments…</p>
                </div>
              ) : filtered.length === 0 ? (
                <div className="p-5 text-center">
                  <p style={{ color: '#4E5953', fontSize: '0.875rem' }}>No segments match filters.</p>
                </div>
              ) : (
                filtered.map((seg) => (
                  <button
                    key={seg.segment_id}
                    onClick={() => setSelected((prev) => (prev?.segment_id === seg.segment_id ? null : seg))}
                    className="w-full flex items-center gap-3 px-4 py-3 text-left hover:bg-muted transition-colors"
                    style={{ background: selected?.segment_id === seg.segment_id ? '#F8F6F2' : 'transparent' }}
                  >
                    <div className="w-2.5 h-2.5 rounded-full flex-shrink-0" style={{ background: LEVEL_COLOR[seg.level] }} />
                    <div className="flex-1 min-w-0">
                      <div style={{ color: '#1F2421', fontWeight: 500, fontSize: '0.875rem' }} className="truncate">
                        {seg.name}
                      </div>
                      <div style={{ color: '#4E5953', fontSize: '0.75rem' }}>{seg.region} · {seg.occupancy_pct}%</div>
                    </div>
                    <div className="flex items-center gap-1">{TREND_ICON(seg.trend)}</div>
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
