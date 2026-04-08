/**
 * OSMMap — React wrapper around MapLibre using OpenStreetMap raster tiles.
 * Search/routing are handled server-side; this component is display only.
 */

import type { CSSProperties } from 'react';
import { useEffect, useRef } from 'react';
import { Map as MapLibreMap, Marker } from 'maplibre-gl';
import type { StyleSpecification } from 'maplibre-gl';
import 'maplibre-gl/dist/maplibre-gl.css';

const OSM_STYLE: StyleSpecification = {
  version: 8,
  sources: {
    osm: {
      type: 'raster',
      tiles: ['https://tile.openstreetmap.org/{z}/{x}/{y}.png'],
      tileSize: 256,
      attribution: '© OpenStreetMap contributors',
    },
  },
  layers: [
    {
      id: 'osm',
      type: 'raster',
      source: 'osm',
      minzoom: 0,
      maxzoom: 19,
    },
  ],
};

export interface OSMMapProps {
  center: [number, number];
  zoom?: number;
  onReady?: (map: MapLibreMap) => void;
  style?: CSSProperties;
  className?: string;
}

export default function OSMMap({ center, zoom = 10, onReady, style, className }: OSMMapProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const mapRef = useRef<MapLibreMap | null>(null);

  useEffect(() => {
    if (!containerRef.current) return;

    const map = new MapLibreMap({
      container: containerRef.current,
      style: OSM_STYLE,
      center: [center[1], center[0]],
      zoom,
    });

    mapRef.current = map;

    const handleLoad = () => {
      onReady?.(map);
    };

    if (map.loaded()) {
      handleLoad();
    } else {
      map.once('load', handleLoad);
    }

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

export function addPolyline(
  map: MapLibreMap,
  id: string,
  coords: [number, number][],
  color = '#2F6B55',
  width = 4,
) {
  const geojson = {
    type: 'Feature' as const,
    geometry: {
      type: 'LineString' as const,
      coordinates: coords.map(([lat, lng]) => [lng, lat]),
    },
    properties: {},
  };

  const existingSource = map.getSource(id) as ({ setData: (data: unknown) => void } | undefined);
  if (existingSource) {
    existingSource.setData(geojson);
    return;
  }

  map.addSource(id, { type: 'geojson', data: geojson });
  map.addLayer({
    id,
    type: 'line',
    source: id,
    layout: { 'line-join': 'round', 'line-cap': 'round' },
    paint: { 'line-color': color, 'line-width': width },
  });
}

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
