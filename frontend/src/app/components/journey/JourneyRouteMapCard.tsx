import { LngLatBounds, Map as MapLibreMap, Marker } from 'maplibre-gl';
import { MapPin } from 'lucide-react';
import { useEffect, useMemo, useRef } from 'react';
import { GeoPoint, Journey } from '../../types';
import OSMMap, { addMarker, addPolyline } from '../ui/OSMMap';

interface JourneyRouteMapCardProps {
  journey: Journey;
  title?: string;
}

function isFiniteCoordinate(value: number | undefined): value is number {
  return typeof value === 'number' && Number.isFinite(value);
}

function appendUniquePoint(points: GeoPoint[], point?: GeoPoint) {
  if (!point) return;
  const last = points[points.length - 1];
  if (last && last.lat === point.lat && last.lng === point.lng) {
    return;
  }
  points.push(point);
}

function segmentPoint(lat: number | undefined, lng: number | undefined): GeoPoint | undefined {
  if (!isFiniteCoordinate(lat) || !isFiniteCoordinate(lng)) {
    return undefined;
  }
  return { lat, lng };
}

function midpoint(a: GeoPoint, b: GeoPoint): GeoPoint {
  return {
    lat: (a.lat + b.lat) / 2,
    lng: (a.lng + b.lng) / 2,
  };
}

export default function JourneyRouteMapCard({ journey, title = 'Route map' }: JourneyRouteMapCardProps) {
  const mapRef = useRef<MapLibreMap | null>(null);
  const markerRefs = useRef<Marker[]>([]);

  const path = useMemo(() => {
    const points: GeoPoint[] = [];

    if (Array.isArray(journey.mapPath)) {
      journey.mapPath.forEach((point) => appendUniquePoint(points, point));
    }

    if (points.length < 2) {
      const sortedSegments = [...journey.segments].sort((a, b) => {
        return (a.sequenceOrder ?? 0) - (b.sequenceOrder ?? 0);
      });
      sortedSegments.forEach((segment) => {
        appendUniquePoint(points, segmentPoint(segment.fromLat, segment.fromLng));
        appendUniquePoint(points, segmentPoint(segment.toLat, segment.toLng));
      });
    }

    if (points.length < 2) {
      appendUniquePoint(points, journey.originCoords);
      appendUniquePoint(points, journey.destinationCoords);
    }

    return points;
  }, [journey.destinationCoords, journey.mapPath, journey.originCoords, journey.segments]);

  const originPoint = useMemo(() => {
    return journey.originCoords ?? path[0];
  }, [journey.originCoords, path]);

  const destinationPoint = useMemo(() => {
    if (journey.destinationCoords) {
      return journey.destinationCoords;
    }
    return path.length > 0 ? path[path.length - 1] : undefined;
  }, [journey.destinationCoords, path]);

  const hasRenderableRoute = !!originPoint && !!destinationPoint;

  const mapCenter: [number, number] = useMemo(() => {
    if (!originPoint || !destinationPoint) {
      return [53.3498, -6.2603];
    }
    const center = midpoint(originPoint, destinationPoint);
    return [center.lat, center.lng];
  }, [destinationPoint, originPoint]);

  const drawOverlay = (map: MapLibreMap) => {
    if (!originPoint || !destinationPoint) return;

    markerRefs.current.forEach((marker) => marker.remove());
    markerRefs.current = [];

    const polyline = path.length >= 2 ? path : [originPoint, destinationPoint];
    addPolyline(
      map,
      'journey-route',
      polyline.map((point) => [point.lat, point.lng]),
      '#2F6B55',
      4,
    );

    markerRefs.current.push(
      addMarker(map, originPoint.lat, originPoint.lng, '#2F6B55'),
      addMarker(map, destinationPoint.lat, destinationPoint.lng, '#B65C3A'),
    );

    const first = polyline[0];
    const bounds = new LngLatBounds([first.lng, first.lat], [first.lng, first.lat]);
    polyline.forEach((point) => {
      bounds.extend([point.lng, point.lat]);
    });
    map.fitBounds(bounds, { padding: 40, maxZoom: 13 });
  };

  const handleMapReady = (map: MapLibreMap) => {
    mapRef.current = map;
    drawOverlay(map);
  };

  useEffect(() => {
    if (!mapRef.current) return;
    drawOverlay(mapRef.current);
    // The map overlay must refresh when route geometry changes.
  }, [path, originPoint, destinationPoint]);

  return (
    <div className="bg-white rounded-xl p-5 mb-4" style={{ border: '1px solid var(--border)' }}>
      <div className="flex items-center gap-2.5 mb-4">
        <MapPin size={16} color="#2F6B55" />
        <h4 style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem' }}>
          {title}
        </h4>
      </div>

      {hasRenderableRoute ? (
        <OSMMap center={mapCenter} zoom={11} onReady={handleMapReady} style={{ minHeight: '260px', borderRadius: '12px' }} />
      ) : (
        <div
          className="rounded-lg p-4 text-sm"
          style={{ background: '#F8F6F2', color: '#4E5953', border: '1px solid var(--border)' }}
        >
          Route coordinates are unavailable for this journey.
        </div>
      )}
    </div>
  );
}
