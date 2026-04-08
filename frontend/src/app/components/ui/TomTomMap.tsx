/**
 * TomTomMap — thin React wrapper around the TomTom Maps Web SDK.
 *
 * Key design decisions:
 *  - Map tiles use VITE_TOMTOM_MAPS_KEY (browser-safe, referrer-locked).
 *  - Search and routing are never called from this component; the server proxy
 *    handles those so the full API key stays server-side.
 *  - The component is deliberately minimal: it just renders a tile map and
 *    exposes an `onReady` callback so parent components can draw their own
 *    overlays (markers, polylines).
 */

import tt from '@tomtom-international/web-sdk-maps';
import '@tomtom-international/web-sdk-maps/dist/maps.css';
import { useEffect, useRef } from 'react';

const MAPS_KEY = import.meta.env.VITE_TOMTOM_MAPS_KEY ?? '';

export interface TomTomMapProps {
  /** Initial centre [lat, lng] */
  center: [number, number];
  zoom?: number;
  /** Called once when the map has loaded, passes the map instance */
  onReady?: (map: tt.Map) => void;
  style?: React.CSSProperties;
  className?: string;
}

export default function TomTomMap({ center, zoom = 10, onReady, style, className }: TomTomMapProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const mapRef = useRef<tt.Map | null>(null);

  useEffect(() => {
    if (!containerRef.current) return;

    const map = tt.map({
      key: MAPS_KEY,
      container: containerRef.current,
      center: { lat: center[0], lng: center[1] },
      zoom,
      stylesVisibility: {
        trafficIncidents: false,
        trafficFlow: false,
      },
    });

    mapRef.current = map;

    map.on('load', () => {
      onReady?.(map);
    });

    return () => {
      map.remove();
      mapRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div
      ref={containerRef}
      className={className}
      style={{ width: '100%', height: '100%', minHeight: '320px', ...style }}
    />
  );
}

// ── Utility helpers for parent components ──────────────────────────────────

/** Draw a polyline on a map. Returns the layer + source IDs so they can be removed. */
export function addPolyline(
  map: tt.Map,
  id: string,
  coords: [number, number][],
  color = '#2F6B55',
  width = 4,
) {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const geojson: any = {
    type: 'Feature',
    geometry: {
      type: 'LineString',
      coordinates: coords.map(([lat, lng]) => [lng, lat]),
    },
    properties: {},
  };

  if (map.getSource(id)) {
    (map.getSource(id) as tt.GeoJSONSource).setData(geojson);
  } else {
    map.addSource(id, { type: 'geojson', data: geojson });
    map.addLayer({
      id,
      type: 'line',
      source: id,
      layout: { 'line-join': 'round', 'line-cap': 'round' },
      paint: { 'line-color': color, 'line-width': width },
    });
  }
}

/** Add a pin marker to the map. */
export function addMarker(map: tt.Map, lat: number, lng: number, color = '#2F6B55'): tt.Marker {
  const el = document.createElement('div');
  el.style.cssText = `
    width: 16px; height: 16px; border-radius: 50%;
    background: ${color}; border: 3px solid white;
    box-shadow: 0 2px 6px rgba(0,0,0,0.3);
  `;
  return new tt.Marker({ element: el }).setLngLat([lng, lat]).addTo(map);
}
