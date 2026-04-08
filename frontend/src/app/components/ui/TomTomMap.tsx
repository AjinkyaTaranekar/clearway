/**
 * TomTomMap — thin React wrapper around the @tomtom-org/maps-sdk TomTomMap class.
 *
 * Key design decisions:
 *  - Map tiles use VITE_TOMTOM_MAPS_KEY (browser-safe, referrer-locked).
 *  - Search and routing are never called from this component; the server proxy
 *    handles those so the full API key stays server-side.
 *  - The component exposes the underlying MapLibre map via `onReady` so parents
 *    can add custom sources, layers, and markers.
 */

import { TomTomMap as TTMap } from '@tomtom-org/maps-sdk/map';
import { Map as MapLibreMap, Marker } from 'maplibre-gl';
import 'maplibre-gl/dist/maplibre-gl.css';
import { useEffect, useRef } from 'react';

const MAPS_KEY = import.meta.env.VITE_TOMTOM_MAPS_KEY ?? '';

export interface TomTomMapProps {
  /** Initial centre [lat, lng] */
  center: [number, number];
  zoom?: number;
  /** Called once the underlying MapLibre map has loaded */
  onReady?: (map: MapLibreMap) => void;
  style?: React.CSSProperties;
  className?: string;
}

export default function TomTomMap({ center, zoom = 10, onReady, style, className }: TomTomMapProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const sdkMapRef = useRef<TTMap | null>(null);

  useEffect(() => {
    if (!containerRef.current) return;

    const sdkMap = new TTMap({
      key: MAPS_KEY,
      mapLibre: {
        container: containerRef.current,
        center: [center[1], center[0]], // MapLibre uses [lng, lat]
        zoom,
      },
    });

    sdkMapRef.current = sdkMap;

    const mlMap = sdkMap.mapLibreMap;

    const onLoad = () => {
      onReady?.(mlMap);
    };

    if (mlMap.loaded()) {
      onLoad();
    } else {
      mlMap.once('load', onLoad);
    }

    return () => {
      mlMap.remove();
      sdkMapRef.current = null;
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

/** Draw or update a polyline on a MapLibre map. */
export function addPolyline(
  map: MapLibreMap,
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
      // MapLibre expects [lng, lat]
      coordinates: coords.map(([lat, lng]) => [lng, lat]),
    },
    properties: {},
  };

  if (map.getSource(id)) {
    (map.getSource(id) as ReturnType<typeof map.getSource> & { setData: (d: unknown) => void })
      ?.setData(geojson);
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

/** Add a circle marker pin to a MapLibre map. */
export function addMarker(map: MapLibreMap, lat: number, lng: number, color = '#2F6B55'): Marker {
  const el = document.createElement('div');
  el.style.cssText = `
    width: 16px; height: 16px; border-radius: 50%;
    background: ${color}; border: 3px solid white;
    box-shadow: 0 2px 6px rgba(0,0,0,0.3);
    cursor: pointer;
  `;
  return new Marker({ element: el }).setLngLat([lng, lat]).addTo(map);
}
