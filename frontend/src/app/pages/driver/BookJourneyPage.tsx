import { LngLatBounds, Map as MapLibreMap, Marker } from 'maplibre-gl';
import { AlertCircle, ChevronRight, Clock, Info } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router';
import PlaceSearch from '../../components/ui/PlaceSearch';
import OSMMap, { addMarker, addPolyline } from '../../components/ui/OSMMap';
import { useApp } from '../../context/AppContext';
import { authHeaders, getToken } from '../../services/auth';
import { iamListVehicles, UserVehicle } from '../../services/iamApi';
import { computeRoute, PlaceResult } from '../../services/mapApi';
import { DEFAULT_REGION_UI_DEFAULTS, getRegionUiDefaults } from '../../services/regionDefaults';

const API_BASE_URL = import.meta.env.VITE_API_URL ?? '';

const VEHICLE_TYPES = {
  car: { api: 'car', value: 'Car', label: 'Car', icon: 'car', desc: 'Standard passenger vehicle' },
  van: { api: 'van', value: 'Van', label: 'Van', icon: 'van', desc: 'Light commercial vehicle' },
  motorcycle: { api: 'motorcycle', value: 'Motorcycle', label: 'Motorcycle', icon: 'motorcycle', desc: 'Two-wheeled vehicle' },
  truck: { api: 'truck', value: 'HGV', label: 'HGV', icon: 'truck', desc: 'Heavy goods vehicle' },
} as const;

type VehicleIconType = (typeof VEHICLE_TYPES)[keyof typeof VEHICLE_TYPES]['icon'];

function VehicleTypeIcon({ icon, size = 22, stroke = '#2F6B55' }: { icon: VehicleIconType; size?: number; stroke?: string }) {
  const bodyFill = '#E8F4ED';
  const accentFill = '#CFE5D8';
  const wheelFill = '#202824';

  if (icon === 'car') {
    return (
      <svg width={size} height={size} viewBox="0 0 64 40" fill="none" aria-hidden="true">
        <path d="M14 18L21 10H42L50 18H14Z" fill={accentFill} stroke={stroke} strokeWidth="2" strokeLinejoin="round" />
        <rect x="7" y="18" width="50" height="13" rx="4" fill={bodyFill} stroke={stroke} strokeWidth="2" />
        <rect x="25" y="12" width="13" height="5" rx="1.5" fill="white" opacity="0.8" />
        <circle cx="20" cy="31" r="4.5" fill={wheelFill} />
        <circle cx="44" cy="31" r="4.5" fill={wheelFill} />
      </svg>
    );
  }

  if (icon === 'van') {
    return (
      <svg width={size} height={size} viewBox="0 0 64 40" fill="none" aria-hidden="true">
        <rect x="6" y="14" width="44" height="17" rx="3" fill={bodyFill} stroke={stroke} strokeWidth="2" />
        <path d="M50 19L56 19L59 24V31H50V19Z" fill={accentFill} stroke={stroke} strokeWidth="2" strokeLinejoin="round" />
        <rect x="11" y="18" width="19" height="7" rx="1.5" fill="white" opacity="0.8" />
        <rect x="34" y="18" width="10" height="7" rx="1.5" fill="white" opacity="0.8" />
        <circle cx="18" cy="31" r="4.5" fill={wheelFill} />
        <circle cx="46" cy="31" r="4.5" fill={wheelFill} />
      </svg>
    );
  }

  if (icon === 'motorcycle') {
    return (
      <svg width={size} height={size} viewBox="0 0 64 40" fill="none" aria-hidden="true">
        <circle cx="16" cy="30" r="6.5" fill={wheelFill} />
        <circle cx="48" cy="30" r="6.5" fill={wheelFill} />
        <path d="M16 30L26 20H36L48 30" stroke={stroke} strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" />
        <path d="M30 20L38 14L43 14" stroke={stroke} strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" />
        <path d="M28 20L24 30" stroke={stroke} strokeWidth="2.5" strokeLinecap="round" />
        <circle cx="36" cy="20" r="3" fill={accentFill} stroke={stroke} strokeWidth="2" />
      </svg>
    );
  }

  if (icon === 'truck') {
    return (
      <svg width={size} height={size} viewBox="0 0 64 40" fill="none" aria-hidden="true">
        <rect x="5" y="14" width="34" height="16" rx="2.5" fill={bodyFill} stroke={stroke} strokeWidth="2" />
        <path d="M39 18H49L57 24V30H39V18Z" fill={accentFill} stroke={stroke} strokeWidth="2" strokeLinejoin="round" />
        <rect x="43" y="20" width="8" height="5" rx="1.5" fill="white" opacity="0.8" />
        <circle cx="16" cy="31" r="4.5" fill={wheelFill} />
        <circle cx="33" cy="31" r="4.5" fill={wheelFill} />
        <circle cx="49" cy="31" r="4.5" fill={wheelFill} />
      </svg>
    );
  }

  return null;
}

const SLOT_INTERVAL_MINUTES = 30;
const SEGMENT_WINDOW_BUFFER_MINUTES = 5;

const VEHICLE_SLOT_WEIGHTS: Record<string, number> = {
  car: 1,
  van: 1.5,
  motorcycle: 0.5,
  truck: 3,
};

const FALLBACK_PRIMARY_VEHICLE_ID = 'primary';

interface FormErrors {
  origin?: string;
  destination?: string;
  departureTime?: string;
  vehicleType?: string;
}

interface RouteCapacitySegment {
  segmentId: string;
  segmentName: string;
  sequence: number;
  traversalMinutes: number;
  fromNodeId: string;
  toNodeId: string;
}

interface CapacityCheckResponse {
  segment_id: string;
  max_capacity: number;
  reserved_slots: number;
  available_slots: number;
  can_reserve: boolean;
  is_closed?: boolean;
  closure_reason?: string;
  closure_start?: string;
  closure_end?: string;
}

interface SlotState {
  status: 'checking' | 'available' | 'exhausted' | 'error';
  message?: string;
  minAvailableSlots?: number;
  segmentChecks?: SegmentFlowItem[];
}

interface SegmentWindow {
  segment: RouteCapacitySegment;
  timeWindowStart: Date;
  timeWindowEnd: Date;
}

interface SegmentFlowItem {
  segmentId: string;
  segmentName: string;
  sequence: number;
  traversalMinutes: number;
  timeWindowStart: string;
  timeWindowEnd: string;
  maxCapacity?: number;
  reservedSlots?: number;
  availableSlots?: number;
  canReserve?: boolean;
  isClosed?: boolean;
  closureReason?: string;
  closureStart?: string;
  closureEnd?: string;
}

interface DriverVehicle {
  id: string;
  vehicleType: string;
  isPrimary: boolean;
  isEmergencyVehicle: boolean;
  licenseNumber: string;
}

interface SlotOption {
  value: string;
  label: string;
}

function formatDateInput(date: Date): string {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const day = String(date.getDate()).padStart(2, '0');
  return `${year}-${month}-${day}`;
}

function buildDaySlots(dateValue: string): SlotOption[] {
  const slots: SlotOption[] = [];
  const totalSlots = (24 * 60) / SLOT_INTERVAL_MINUTES;

  for (let i = 0; i < totalSlots; i += 1) {
    const totalMinutes = i * SLOT_INTERVAL_MINUTES;
    const hours = String(Math.floor(totalMinutes / 60)).padStart(2, '0');
    const minutes = String(totalMinutes % 60).padStart(2, '0');
    slots.push({
      value: `${dateValue}T${hours}:${minutes}`,
      label: `${hours}:${minutes}`,
    });
  }

  return slots;
}

function buildSegmentWindows(slotValue: string, segments: RouteCapacitySegment[]): SegmentWindow[] {
  const departure = new Date(slotValue);
  if (Number.isNaN(departure.getTime())) return [];

  let cursor = departure;
  const bufferMs = SEGMENT_WINDOW_BUFFER_MINUTES * 60 * 1000;
  return segments.map((segment) => {
    const plannedStart = new Date(cursor);
    const plannedEnd = new Date(cursor.getTime() + segment.traversalMinutes * 60 * 1000);
    const timeWindowStart = new Date(plannedStart.getTime() - bufferMs);
    const timeWindowEnd = new Date(plannedEnd.getTime() + bufferMs);
    cursor = plannedEnd;
    return {
      segment,
      timeWindowStart,
      timeWindowEnd,
    };
  });
}

function formatClock(ts: string | Date): string {
  const date = ts instanceof Date ? ts : new Date(ts);
  if (Number.isNaN(date.getTime())) return '--:--';
  return date.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit', hour12: false });
}

function formatSlots(slots: number): string {
  if (Number.isInteger(slots)) return String(slots);
  return slots.toFixed(1);
}

export default function BookJourneyPage() {
  const navigate = useNavigate();
  const { bookJourney, user } = useApp();

  const [step, setStep] = useState(1);
  const [originPlace, setOriginPlace] = useState<PlaceResult | null>(null);
  const [destPlace, setDestPlace] = useState<PlaceResult | null>(null);
  const [selectedDate, setSelectedDate] = useState(formatDateInput(new Date()));
  const [departureTime, setDepartureTime] = useState('');
  const [availableVehicles, setAvailableVehicles] = useState<DriverVehicle[]>([]);
  const [selectedVehicleID, setSelectedVehicleID] = useState('');
  const [vehiclesLoading, setVehiclesLoading] = useState(false);
  const [vehiclesError, setVehiclesError] = useState('');
  const [errors, setErrors] = useState<FormErrors>({});
  const [submitting, setSubmitting] = useState(false);
  const [routeSegments, setRouteSegments] = useState<RouteCapacitySegment[]>([]);
  const [routeLoading, setRouteLoading] = useState(false);
  const [routeStatusMessage, setRouteStatusMessage] = useState('');
  const [routeTotalMinutes, setRouteTotalMinutes] = useState(0);
  const [routePolyline, setRoutePolyline] = useState<[number, number][]>([]);
  const [slotStates, setSlotStates] = useState<Record<string, SlotState>>({});
  const [ghostNotice, setGhostNotice] = useState('');
  const [regionDefaults, setRegionDefaults] = useState(DEFAULT_REGION_UI_DEFAULTS);

  const mapRef = useRef<MapLibreMap | null>(null);
  const markerRefs = useRef<Marker[]>([]);

  const minBookDate = useMemo(() => formatDateInput(new Date()), []);
  const maxBookDate = useMemo(
    () => formatDateInput(new Date(Date.now() + 14 * 24 * 60 * 60 * 1000)),
    [],
  );
  const daySlots = useMemo(() => {
    const slots = buildDaySlots(selectedDate);
    const today = formatDateInput(new Date());
    if (selectedDate !== today) {
      return slots;
    }

    const now = Date.now();
    return slots.filter((slot) => {
      const slotTime = new Date(slot.value).getTime();
      return !Number.isNaN(slotTime) && slotTime >= now;
    });
  }, [selectedDate]);
  const selectedVehicle = useMemo(
    () => availableVehicles.find((vehicle) => vehicle.id === selectedVehicleID) ?? null,
    [availableVehicles, selectedVehicleID],
  );
  const selectedVehicleMeta = useMemo(() => {
    if (!selectedVehicle) return null;
    return VEHICLE_TYPES[selectedVehicle.vehicleType as keyof typeof VEHICLE_TYPES] ?? null;
  }, [selectedVehicle]);
  const requiredSlots = selectedVehicle ? (VEHICLE_SLOT_WEIGHTS[selectedVehicle.vehicleType] ?? 1) : 1;
  const selectedSlotState = departureTime ? slotStates[departureTime] : undefined;

  useEffect(() => {
    let cancelled = false;

    const loadUserVehicles = async () => {
      const token = getToken();
      if (!token) return;

      setVehiclesLoading(true);
      setVehiclesError('');

      const fallbackType = (user?.vehicle_type ?? '').toLowerCase();
      const fallbackVehicles: DriverVehicle[] = VEHICLE_TYPES[fallbackType as keyof typeof VEHICLE_TYPES]
        ? [{
            id: FALLBACK_PRIMARY_VEHICLE_ID,
            vehicleType: fallbackType,
            isPrimary: true,
            isEmergencyVehicle: false,
            licenseNumber: '',
          }]
        : [];

      try {
        const items = await iamListVehicles(token);
        if (cancelled) return;

        const mappedVehicles: DriverVehicle[] = items
          .map((vehicle: UserVehicle) => ({
            id: vehicle.id,
            vehicleType: vehicle.vehicle_type.toLowerCase(),
            isPrimary: vehicle.is_primary,
            isEmergencyVehicle: vehicle.is_emergency_vehicle,
            licenseNumber: vehicle.license_info?.license_number ?? '',
          }))
          .filter((vehicle) => Boolean(VEHICLE_TYPES[vehicle.vehicleType as keyof typeof VEHICLE_TYPES]));

        const nextVehicles = mappedVehicles.length > 0 ? mappedVehicles : fallbackVehicles;
        setAvailableVehicles(nextVehicles);
        setSelectedVehicleID((current) => {
          if (nextVehicles.some((vehicle) => vehicle.id === current)) return current;
          return nextVehicles.find((vehicle) => vehicle.isPrimary)?.id ?? nextVehicles[0]?.id ?? '';
        });

        if (mappedVehicles.length === 0 && fallbackVehicles.length > 0) {
          setVehiclesError('Using your primary profile vehicle because IAM vehicle list is empty.');
        }
      } catch {
        if (cancelled) return;
        setAvailableVehicles(fallbackVehicles);
        setSelectedVehicleID(fallbackVehicles[0]?.id ?? '');
        setVehiclesError('Could not load your saved vehicles. Showing fallback primary vehicle only.');
      } finally {
        if (!cancelled) {
          setVehiclesLoading(false);
        }
      }
    };

    void loadUserVehicles();

    return () => {
      cancelled = true;
    };
  }, [user?.id, user?.vehicle_type]);

  useEffect(() => {
    let cancelled = false;
    getRegionUiDefaults()
      .then((defaults) => {
        if (!cancelled) {
          setRegionDefaults(defaults);
        }
      })
      .catch(() => {
        // Keep generic defaults when region detection is unavailable.
      });

    return () => {
      cancelled = true;
    };
  }, []);

  const segmentFlow = useMemo(() => {
    if (!departureTime || routeSegments.length === 0) return [];

    const windows = buildSegmentWindows(departureTime, routeSegments);
    const checksBySegment = new Map(
      (selectedSlotState?.segmentChecks ?? []).map((check) => [check.segmentId, check]),
    );

    return windows.map((window) => {
      const check = checksBySegment.get(window.segment.segmentId);
      return {
        segmentId: window.segment.segmentId,
        segmentName: window.segment.segmentName,
        sequence: window.segment.sequence,
        traversalMinutes: window.segment.traversalMinutes,
        timeWindowStart: window.timeWindowStart.toISOString(),
        timeWindowEnd: window.timeWindowEnd.toISOString(),
        maxCapacity: check?.maxCapacity,
        reservedSlots: check?.reservedSlots,
        availableSlots: check?.availableSlots,
        canReserve: check?.availableSlots !== undefined ? check.availableSlots >= requiredSlots : check?.canReserve,
      } as SegmentFlowItem;
    });
  }, [departureTime, routeSegments, selectedSlotState, requiredSlots]);

  const etaTimeLabel = useMemo(() => {
    if (segmentFlow.length === 0) return '';
    return formatClock(segmentFlow[segmentFlow.length - 1].timeWindowEnd);
  }, [segmentFlow]);

  const isSlotWithinAdvanceWindow = (slotValue: string): boolean => {
    const slotDate = new Date(slotValue);
    return slotDate.getTime() >= Date.now() + 55 * 60 * 1000;
  };

  useEffect(() => {
    let cancelled = false;

    const resolveRouteSegments = async () => {
      setSlotStates({});
      setGhostNotice('');

      if (!originPlace || !destPlace || originPlace.place_id === destPlace.place_id) {
        setRouteSegments([]);
        setRouteTotalMinutes(0);
        setRoutePolyline([]);
        setRouteStatusMessage('');
        setRouteLoading(false);
        return;
      }

      setRouteLoading(true);
      setRouteStatusMessage('Resolving route for live slot checks...');

      try {
        const route = await computeRoute(
          { lat: originPlace.lat, lng: originPlace.lng },
          { lat: destPlace.lat, lng: destPlace.lng },
        );
        const sortedSegments = [...route.segments].sort((a, b) => (a.sequence || 0) - (b.sequence || 0));
        const mappedSegments = sortedSegments.map((segment, index) => ({
          segmentId: segment.segment_id,
          segmentName: segment.segment_name,
          sequence: segment.sequence || index + 1,
          traversalMinutes: Math.max(1, segment.traversal_time_minutes),
          fromNodeId: segment.from_node_id,
          toNodeId: segment.to_node_id,
        }));

        if (mappedSegments.length === 0) {
          throw new Error('Route could not be resolved');
        }

        const polylineCoords: [number, number][] = (route.path ?? [])
          .map((point) => [point.lat, point.lng] as [number, number]);

        if (polylineCoords.length < 2) {
          polylineCoords.push([originPlace.lat, originPlace.lng], [destPlace.lat, destPlace.lng]);
        }

        const totalMinutes = Math.max(
          route.total_duration_minutes || route.total_traversal_time_minutes || 0,
          mappedSegments.reduce((sum, segment) => sum + segment.traversalMinutes, 0),
        );

        if (!cancelled) {
          setRouteSegments(mappedSegments);
          setRouteTotalMinutes(totalMinutes);
          setRoutePolyline(polylineCoords);
          setRouteStatusMessage(
            `Live capacity checks are enabled across ${mappedSegments.length} segments (~${totalMinutes} min total).`,
          );
        }
      } catch {
        if (!cancelled) {
          setRouteSegments([]);
          setRouteTotalMinutes(0);
          setRoutePolyline([]);
          setRouteStatusMessage('Live slot checks are unavailable. Final capacity validation still happens on submit.');
        }
      } finally {
        if (!cancelled) {
          setRouteLoading(false);
        }
      }
    };

    void resolveRouteSegments();

    return () => {
      cancelled = true;
    };
  }, [originPlace, destPlace]);

  const checkSlotCapacity = async (slotValue: string, forceRefresh = false): Promise<SlotState> => {
    const existing = slotStates[slotValue];
    if (!forceRefresh && existing && existing.status !== 'error') {
      return existing;
    }

    setSlotStates((prev) => ({
      ...prev,
      [slotValue]: { status: 'checking' },
    }));

    try {
      const windows = buildSegmentWindows(slotValue, routeSegments);
      if (windows.length === 0) {
        throw new Error('invalid slot datetime');
      }

      const checks = await Promise.all(
        windows.map(async (window) => {
          const query = new URLSearchParams({
            segment_id: window.segment.segmentId,
            time_window_start: window.timeWindowStart.toISOString(),
            time_window_end: window.timeWindowEnd.toISOString(),
          });
          if (selectedVehicle?.isEmergencyVehicle) {
            query.set('priority_level', 'max');
          }

          const res = await fetch(`${API_BASE_URL}/api/v1/capacity/check?${query.toString()}`, {
            headers: {
              ...authHeaders(),
            },
          });

          if (!res.ok) {
            throw new Error(`capacity check failed (${res.status})`);
          }

          const check = (await res.json()) as CapacityCheckResponse;
          const isClosed = check.is_closed === true;
          return {
            segmentId: window.segment.segmentId,
            segmentName: window.segment.segmentName,
            sequence: window.segment.sequence,
            traversalMinutes: window.segment.traversalMinutes,
            timeWindowStart: window.timeWindowStart.toISOString(),
            timeWindowEnd: window.timeWindowEnd.toISOString(),
            maxCapacity: check.max_capacity,
            reservedSlots: check.reserved_slots,
            availableSlots: check.available_slots,
            canReserve: !isClosed && (check.available_slots ?? 0) >= requiredSlots,
            isClosed,
            closureReason: check.closure_reason,
            closureStart: check.closure_start,
            closureEnd: check.closure_end,
          } as SegmentFlowItem;
        }),
      );

      const minAvailableSlots = checks.reduce(
        (minimum, check) => Math.min(minimum, check.availableSlots ?? 0),
        Number.POSITIVE_INFINITY,
      );

      const firstClosed = checks.find((segment) => segment.isClosed);
      if (firstClosed || minAvailableSlots < requiredSlots) {
        if (firstClosed) {
          const closeWindow = `${formatClock(firstClosed.timeWindowStart)}-${formatClock(firstClosed.timeWindowEnd)}`;
          const closureEnd = firstClosed.closureEnd ? ` until ${formatClock(firstClosed.closureEnd)}` : '';
          const closureReason = firstClosed.closureReason ? ` (${firstClosed.closureReason})` : '';

          const exhausted: SlotState = {
            status: 'exhausted',
            minAvailableSlots,
            segmentChecks: checks,
            message: `${firstClosed.segmentName} is closed for ${closeWindow}${closureEnd}${closureReason}. Choose a different departure slot.`,
          };
          setSlotStates((prev) => ({ ...prev, [slotValue]: exhausted }));
          return exhausted;
        }

        const firstBlocked = checks.find((segment) => (segment.availableSlots ?? 0) < requiredSlots);
        const blockedSegmentLabel = firstBlocked
          ? `${firstBlocked.segmentName} (${formatClock(firstBlocked.timeWindowStart)}-${formatClock(firstBlocked.timeWindowEnd)})`
          : 'one or more route segments';

        const exhausted: SlotState = {
          status: 'exhausted',
          minAvailableSlots,
          segmentChecks: checks,
          message: `That slot is now full on ${blockedSegmentLabel}. Another driver may have reserved it first (ghost reservation). Pick a different slot.`,
        };
        setSlotStates((prev) => ({ ...prev, [slotValue]: exhausted }));
        return exhausted;
      }

      const available: SlotState = {
        status: 'available',
        minAvailableSlots,
        segmentChecks: checks,
        message: `All ${checks.length} route segments can reserve ${formatSlots(requiredSlots)} slots for this departure.`,
      };
      setSlotStates((prev) => ({ ...prev, [slotValue]: available }));
      return available;
    } catch {
      const errorState: SlotState = {
        status: 'error',
        message: 'Could not verify live capacity for this slot. Booking will still be validated on submit.',
      };
      setSlotStates((prev) => ({ ...prev, [slotValue]: errorState }));
      return errorState;
    }
  };

  const handleSelectSlot = async (slotValue: string) => {
    if (!isSlotWithinAdvanceWindow(slotValue)) return;

    if (departureTime === slotValue) {
      setDepartureTime('');
      setGhostNotice('');
      setErrors((prev) => ({ ...prev, departureTime: undefined }));
      return;
    }

    setErrors((prev) => ({ ...prev, departureTime: undefined }));
    setGhostNotice('');

    if (routeSegments.length > 0) {
      const check = await checkSlotCapacity(slotValue, true);
      if (check.status === 'exhausted') {
        setDepartureTime('');
        setGhostNotice(check.message ?? 'Selected slot is no longer available.');
        setErrors((prev) => ({
          ...prev,
          departureTime: check.message ?? 'Selected slot is no longer available.',
        }));
        return;
      }
      if (check.status === 'error' && check.message) {
        setGhostNotice(check.message);
      }
    }

    setDepartureTime(slotValue);
  };

  useEffect(() => {
    if (!departureTime || routeSegments.length === 0) return;
    void checkSlotCapacity(departureTime, true);
  }, [departureTime, routeSegments, requiredSlots, selectedVehicleID, selectedVehicle?.isEmergencyVehicle]);

  const validateStep1 = (): FormErrors => {
    const e: FormErrors = {};
    if (!originPlace) e.origin = 'Please select an origin.';
    if (!destPlace) e.destination = 'Please select a destination.';
    if (originPlace && destPlace && originPlace.place_id === destPlace.place_id) {
      e.destination = 'Origin and destination must be different.';
    }
    if (!departureTime) e.departureTime = 'Please choose a departure time.';
    else if (new Date(departureTime) < new Date(Date.now() + 55 * 60 * 1000)) {
      e.departureTime = 'Departure must be at least 1 hour from now.';
    } else if (slotStates[departureTime]?.status === 'exhausted') {
      e.departureTime = slotStates[departureTime]?.message ?? 'Selected slot is no longer available.';
    }
    if (!selectedVehicle) e.vehicleType = 'Please select one of your saved vehicles.';
    return e;
  };

  const handleNext = () => {
    const e = validateStep1();
    if (Object.keys(e).length > 0) { setErrors(e); return; }
    setErrors({});
    setStep(2);
  };

  // Draw route preview on step 2 once map is ready
  const handleMapReady = (map: MapLibreMap) => {
    mapRef.current = map;
    drawRouteOverlay(map);
  };

  // Re-draw if places change while on step 2
  useEffect(() => {
    if (step !== 2 || !mapRef.current) return;
    drawRouteOverlay(mapRef.current);
  }, [step, originPlace, destPlace, routePolyline]);

  const drawRouteOverlay = (map: MapLibreMap) => {
    if (!originPlace || !destPlace) return;

    markerRefs.current.forEach((marker) => marker.remove());
    markerRefs.current = [];

    const polylineCoords: [number, number][] = routePolyline.length >= 2
      ? routePolyline
      : [[originPlace.lat, originPlace.lng], [destPlace.lat, destPlace.lng]];

    addPolyline(
      map,
      'route-preview',
      polylineCoords,
      '#2F6B55',
      4,
    );

    markerRefs.current.push(
      addMarker(map, originPlace.lat, originPlace.lng, '#2F6B55'),
      addMarker(map, destPlace.lat, destPlace.lng, '#B65C3A'),
    );

    const [firstLat, firstLng] = polylineCoords[0];
    const bounds = new LngLatBounds([firstLng, firstLat], [firstLng, firstLat]);
    polylineCoords.forEach(([lat, lng]) => {
      bounds.extend([lng, lat]);
    });

    map.fitBounds(bounds, { padding: 60, maxZoom: 13 });
  };

  const handleSubmit = async () => {
    const e = validateStep1();
    if (Object.keys(e).length > 0 || !originPlace || !destPlace || !selectedVehicle) {
      setErrors(e);
      setStep(1);
      return;
    }

    setSubmitting(true);
    try {
      if (routeSegments.length > 0) {
        const latestSlotState = await checkSlotCapacity(departureTime, true);
        if (latestSlotState.status === 'exhausted') {
          setErrors((prev) => ({
            ...prev,
            departureTime: latestSlotState.message ?? 'Selected slot is no longer available.',
          }));
          setGhostNotice(latestSlotState.message ?? 'Selected slot is no longer available.');
          setStep(1);
          return;
        }
      }

      await bookJourney({
        origin: originPlace.name,
        destination: destPlace.name,
        originCoords: { lat: originPlace.lat, lng: originPlace.lng },
        destCoords: { lat: destPlace.lat, lng: destPlace.lng },
        // Persist canonical place IDs returned by map search.
        originPlaceId: originPlace.place_id,
        destinationPlaceId: destPlace.place_id,
        departureTime,
        vehicleType: selectedVehicle.vehicleType,
        priorityLevel: selectedVehicle.isEmergencyVehicle ? 'max' : 'normal',
      });
      navigate('/driver/booking-result');
    } catch {
      navigate('/driver/booking-result');
    } finally {
      setSubmitting(false);
    }
  };

  const formatDateTime = (dt: string) => {
    if (!dt) return '';
    try {
      return new Date(dt).toLocaleString('en-GB', {
        weekday: 'short', day: 'numeric', month: 'short',
        hour: '2-digit', minute: '2-digit',
      });
    } catch { return dt; }
  };

  // Map centre: midpoint between O and D, otherwise region-aware default.
  const mapCenter: [number, number] =
    originPlace && destPlace
      ? [(originPlace.lat + destPlace.lat) / 2, (originPlace.lng + destPlace.lng) / 2]
      : originPlace
        ? [originPlace.lat, originPlace.lng]
        : regionDefaults.mapCenter;

  return (
    <div className="p-5 lg:p-8 max-w-2xl mx-auto">
      <div className="mb-7">
        <h1 style={{ fontFamily: 'var(--font-heading)', fontWeight: 700, color: '#1F2421', marginBottom: '4px' }}>
          Book a journey
        </h1>
        <p style={{ color: '#4E5953', fontSize: '0.9375rem' }}>
          We'll check road availability on your route before confirming.
        </p>
      </div>

      {/* Step indicator */}
      <div className="flex items-center gap-2 mb-7">
        {[1, 2].map((s) => (
          <div key={s} className="flex items-center gap-2">
            <div
              className="w-7 h-7 rounded-full flex items-center justify-center text-sm transition-colors"
              style={{
                background: step >= s ? '#2F6B55' : '#F0EDE7',
                color: step >= s ? 'white' : '#4E5953',
                fontWeight: 600,
                fontFamily: 'var(--font-heading)',
              }}
            >
              {s}
            </div>
            <span style={{ fontSize: '0.875rem', fontWeight: step === s ? 600 : 400, color: step === s ? '#1F2421' : '#4E5953' }}>
              {s === 1 ? 'Journey details' : 'Review & submit'}
            </span>
            {s < 2 && <ChevronRight size={14} color="#4E5953" />}
          </div>
        ))}
      </div>

      <div className="bg-white rounded-2xl p-6" style={{ border: '1px solid var(--border)' }}>
        {step === 1 ? (
          <div>
            <div className="flex items-start gap-3 p-3.5 rounded-lg mb-6" style={{ background: '#F0EDE7' }}>
              <Info size={15} color="#4E5953" className="flex-shrink-0 mt-0.5" />
              <p style={{ color: '#4E5953', fontSize: '0.8125rem', lineHeight: 1.55 }}>
                You must book at least <strong style={{ color: '#1F2421' }}>1 hour before departure</strong>. Journeys are checked against live road capacity - peak hours (07:00–09:00) on busy routes may be rejected.
              </p>
            </div>

            <div className="space-y-5">
              {/* Origin */}
              <div>
                <label htmlFor="origin" className="block mb-1.5" style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>
                  From
                </label>
                <PlaceSearch
                  id="origin"
                  placeholder={regionDefaults.originPlaceholder}
                  value={originPlace}
                  onChange={(p) => {
                    setOriginPlace(p);
                    if (p && p.place_id === destPlace?.place_id) setDestPlace(null);
                  }}
                  pinColor="#4E5953"
                  error={errors.origin}
                />
                {errors.origin && (
                  <p className="mt-1.5 text-sm flex items-center gap-1" style={{ color: '#B42318' }}>
                    <AlertCircle size={13} /> {errors.origin}
                  </p>
                )}
              </div>

              {/* Destination */}
              <div>
                <label htmlFor="destination" className="block mb-1.5" style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>
                  To
                </label>
                <PlaceSearch
                  id="destination"
                  placeholder={regionDefaults.destinationPlaceholder}
                  value={destPlace}
                  onChange={setDestPlace}
                  pinColor="#B65C3A"
                  error={errors.destination}
                />
                {errors.destination && (
                  <p className="mt-1.5 text-sm flex items-center gap-1" style={{ color: '#B42318' }}>
                    <AlertCircle size={13} /> {errors.destination}
                  </p>
                )}
              </div>

              {/* Departure slots */}
              <div>
                <label htmlFor="departure-date" className="block mb-1.5" style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>
                  Departure date
                </label>
                <input
                  id="departure-date"
                  type="date"
                  value={selectedDate}
                  min={minBookDate}
                  max={maxBookDate}
                  onChange={(e) => {
                    const nextDate = e.target.value;
                    setSelectedDate(nextDate);
                    setGhostNotice('');
                    setErrors((prev) => ({ ...prev, departureTime: undefined }));
                    if (departureTime && !departureTime.startsWith(nextDate)) {
                      setDepartureTime('');
                    }
                  }}
                  className="w-full px-3 py-2.5 rounded-lg outline-none mb-3"
                  style={{
                    border: '1.5px solid var(--border)',
                    background: 'white',
                    color: '#1F2421',
                  }}
                />

                <div className="flex items-center gap-2 mb-1.5">
                  <Clock size={15} color="#4E5953" />
                  <p style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>
                    Departure slot (30-minute intervals)
                  </p>
                </div>

                <div className="grid grid-cols-3 sm:grid-cols-4 gap-2 max-h-60 overflow-y-auto pr-1">
                  {daySlots.map((slot) => {
                    const slotState = slotStates[slot.value];
                    const tooSoon = !isSlotWithinAdvanceWindow(slot.value);
                    const isSelected = departureTime === slot.value;
                    const isChecking = slotState?.status === 'checking';
                    const isExhausted = slotState?.status === 'exhausted';
                    const isAvailable = slotState?.status === 'available';

                    const statusLabel = isSelected
                      ? 'Selected'
                      : tooSoon
                        ? 'Too soon'
                        : isChecking
                          ? 'Checking...'
                          : isExhausted
                            ? 'Full'
                            : isAvailable
                              ? 'Open'
                              : 'Tap to select';

                    let border = '1.5px solid var(--border)';
                    let background = 'white';
                    let color = '#1F2421';

                    if (tooSoon) {
                      border = '1.5px solid #E3DED4';
                      background = '#F8F6F2';
                      color = '#9AA19C';
                    } else if (isChecking) {
                      border = '1.5px solid #E7B46A';
                      background = '#FFF4E0';
                      color = '#7A4500';
                    } else if (isExhausted) {
                      border = '1.5px solid #F5C2BE';
                      background = '#FDECEA';
                      color = '#8E1B13';
                    } else if (isSelected) {
                      border = '1.5px solid #2F6B55';
                      background = '#2F6B55';
                      color = 'white';
                    } else if (isAvailable) {
                      border = '1.5px solid #9AD5AF';
                      background = '#F1FBF4';
                      color = '#1E6639';
                    }

                    const disabled = (tooSoon || isChecking || isExhausted || submitting) && !isSelected;

                    return (
                      <button
                        key={slot.value}
                        type="button"
                        onClick={() => { void handleSelectSlot(slot.value); }}
                        disabled={disabled}
                        className="rounded-lg px-2.5 py-2 text-left transition-colors"
                        style={{ border, background, color }}
                      >
                        <div style={{ fontSize: '0.875rem', fontWeight: 600, lineHeight: 1.2 }}>{slot.label}</div>
                        <div style={{ fontSize: '0.6875rem', opacity: 0.9, marginTop: '2px' }}>{statusLabel}</div>
                      </button>
                    );
                  })}
                </div>

                {daySlots.length === 0 && (
                  <p className="mt-2" style={{ color: '#4E5953', fontSize: '0.75rem' }}>
                    No departure slots remain for today. Please choose another date.
                  </p>
                )}

                <p className="mt-2" style={{ color: '#4E5953', fontSize: '0.75rem' }}>
                  Tap a selected slot again to unselect it.
                </p>

                {routeLoading && (
                  <p className="mt-2" style={{ color: '#7A4500', fontSize: '0.75rem' }}>
                    Resolving route for live slot checks...
                  </p>
                )}

                {!routeLoading && routeStatusMessage && (
                  <p className="mt-2" style={{ color: '#4E5953', fontSize: '0.75rem' }}>
                    {routeStatusMessage}
                  </p>
                )}

                {routeSegments.length > 0 && (
                  <div className="mt-3 p-3 rounded-lg" style={{ background: '#F8F6F2', border: '1px solid var(--border)' }}>
                    <div className="flex items-center justify-between gap-2 mb-2">
                      <p style={{ color: '#1F2421', fontSize: '0.8125rem', fontWeight: 600 }}>
                        Computed route
                      </p>
                      <p style={{ color: '#4E5953', fontSize: '0.75rem' }}>
                        {routeSegments.length} segments · ~{routeTotalMinutes} min
                      </p>
                    </div>
                    <div className="space-y-1.5 max-h-32 overflow-y-auto pr-1">
                      {routeSegments.map((segment) => (
                        <div key={segment.segmentId} className="flex items-center justify-between gap-2">
                          <span style={{ color: '#1F2421', fontSize: '0.75rem' }}>
                            {segment.sequence}. {segment.segmentName}
                          </span>
                          <span style={{ color: '#4E5953', fontSize: '0.75rem' }}>
                            {segment.traversalMinutes} min
                          </span>
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                {departureTime && routeSegments.length > 0 && (
                  <div className="mt-3 p-3 rounded-lg" style={{ background: '#F1FBF4', border: '1px solid #9AD5AF' }}>
                    <div className="flex items-center justify-between gap-2 mb-2">
                      <p style={{ color: '#1E6639', fontSize: '0.8125rem', fontWeight: 600 }}>
                        Segment booking flow
                      </p>
                      {etaTimeLabel && (
                        <p style={{ color: '#1E6639', fontSize: '0.75rem' }}>
                          ETA {etaTimeLabel}
                        </p>
                      )}
                    </div>

                    {selectedSlotState?.status === 'checking' && (
                      <p style={{ color: '#4E5953', fontSize: '0.75rem' }}>
                        Checking segment availability...
                      </p>
                    )}

                    <div className="space-y-2 max-h-56 overflow-y-auto pr-1">
                      {segmentFlow.map((segment) => {
                        const isBlocked = segment.canReserve === false;
                        const hasCapacityData = segment.availableSlots !== undefined;
                        const availabilityLabel = segment.availableSlots === undefined
                          ? 'Checking...'
                          : segment.maxCapacity !== undefined
                            ? `${formatSlots(segment.availableSlots)} free of ${formatSlots(segment.maxCapacity)}`
                            : `${formatSlots(segment.availableSlots)} free`;

                        return (
                          <div
                            key={segment.segmentId}
                            className="rounded-md p-2.5"
                            style={{
                              background: isBlocked ? '#FDECEA' : 'white',
                              border: `1px solid ${isBlocked ? '#F5C2BE' : 'var(--border)'}`,
                            }}
                          >
                            <div className="flex items-center justify-between gap-2">
                              <span style={{ color: '#1F2421', fontSize: '0.75rem', fontWeight: 600 }}>
                                {segment.sequence}. {segment.segmentName}
                              </span>
                              <span style={{ color: '#4E5953', fontSize: '0.75rem' }}>
                                {formatClock(segment.timeWindowStart)}-{formatClock(segment.timeWindowEnd)}
                              </span>
                            </div>
                            <div className="flex items-center justify-between gap-2 mt-1">
                              <span style={{ color: '#4E5953', fontSize: '0.75rem' }}>
                                {segment.traversalMinutes} min traversal
                              </span>
                              <span
                                style={{
                                  color: isBlocked
                                    ? '#B42318'
                                    : hasCapacityData
                                      ? '#1E6639'
                                      : '#4E5953',
                                  fontSize: '0.75rem',
                                  fontWeight: 600,
                                }}
                              >
                                {availabilityLabel}
                              </span>
                            </div>
                          </div>
                        );
                      })}
                    </div>
                  </div>
                )}


                {ghostNotice && (
                  <p className="mt-2 text-sm flex items-center gap-1" style={{ color: '#B42318' }}>
                    <AlertCircle size={13} /> {ghostNotice}
                  </p>
                )}

                {errors.departureTime && (
                  <p className="mt-2 text-sm flex items-center gap-1" style={{ color: '#B42318' }}>
                    <AlertCircle size={13} /> {errors.departureTime}
                  </p>
                )}
              </div>

              {/* Vehicle */}
              <div>
                <label className="block mb-2" style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>
                  Your vehicles
                </label>
                {vehiclesLoading && (
                  <p className="mb-2 text-sm" style={{ color: '#4E5953' }}>
                    Loading your saved vehicles...
                  </p>
                )}
                {vehiclesError && (
                  <p className="mb-2 text-sm flex items-center gap-1" style={{ color: '#7A4500' }}>
                    <AlertCircle size={13} /> {vehiclesError}
                  </p>
                )}
                {errors.vehicleType && (
                  <p className="mb-2 text-sm flex items-center gap-1" style={{ color: '#B42318' }}>
                    <AlertCircle size={13} /> {errors.vehicleType}
                  </p>
                )}
                {availableVehicles.length === 0 ? (
                  <div className="rounded-lg p-3" style={{ border: '1px solid #F5C2BE', background: '#FDECEA', color: '#8E1B13', fontSize: '0.8125rem' }}>
                    No vehicles are available for booking. Add a vehicle in Settings first.
                  </div>
                ) : (
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                    {availableVehicles.map((vehicle) => {
                      const vehicleMeta = VEHICLE_TYPES[vehicle.vehicleType as keyof typeof VEHICLE_TYPES];
                      if (!vehicleMeta) return null;

                      const isSelected = selectedVehicleID === vehicle.id;
                      return (
                        <button
                          key={vehicle.id}
                          type="button"
                          onClick={() => setSelectedVehicleID(vehicle.id)}
                          className="flex items-center gap-3 p-3 rounded-xl text-left transition-all"
                          style={{
                            border: isSelected
                              ? '1.5px solid #2F6B55'
                              : errors.vehicleType ? '1.5px solid #B42318' : '1.5px solid var(--border)',
                            background: isSelected ? '#E8F4ED' : 'white',
                          }}
                        >
                          <span
                            className="h-11 w-11 rounded-lg flex items-center justify-center flex-shrink-0"
                            style={{
                              background: isSelected ? '#DCEFE4' : '#F8F6F2',
                              border: '1px solid #DCE5DE',
                            }}
                          >
                            <VehicleTypeIcon
                              icon={vehicleMeta.icon}
                              size={24}
                              stroke={isSelected ? '#1E6639' : '#2F6B55'}
                            />
                          </span>
                          <div>
                            <div style={{ fontWeight: 600, color: '#1F2421', fontSize: '0.875rem', fontFamily: 'var(--font-heading)' }}>
                              {vehicleMeta.label}
                              {vehicle.isPrimary ? ' · Primary' : ' · Secondary'}
                            </div>
                            <div style={{ color: '#4E5953', fontSize: '0.75rem' }}>
                              {vehicleMeta.desc}
                            </div>
                            <div style={{ color: vehicle.isEmergencyVehicle ? '#1E6639' : '#7A4500', fontSize: '0.75rem' }}>
                              {vehicle.isEmergencyVehicle ? 'Emergency priority enabled' : 'Standard priority'}
                            </div>
                            {vehicle.licenseNumber && (
                              <div style={{ color: '#4E5953', fontSize: '0.75rem' }}>
                                License: {vehicle.licenseNumber}
                              </div>
                            )}
                          </div>
                        </button>
                      );
                    })}
                  </div>
                )}
              </div>
            </div>

            <div className="flex gap-3 mt-7">
              <button
                onClick={() => navigate('/driver')}
                className="px-5 py-2.5 rounded-lg text-sm transition-colors"
                style={{ border: '1.5px solid var(--border)', color: '#4E5953', background: 'white' }}
              >
                Cancel
              </button>
              <button
                onClick={handleNext}
                className="flex-1 py-2.5 rounded-lg text-white text-sm transition-colors flex items-center justify-center gap-2"
                style={{ background: '#2F6B55', fontWeight: 600 }}
                onMouseEnter={(e) => { e.currentTarget.style.background = '#245343'; }}
                onMouseLeave={(e) => { e.currentTarget.style.background = '#2F6B55'; }}
              >
                Review journey <ChevronRight size={16} />
              </button>
            </div>
          </div>
        ) : (
          <div>
            <h3 style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', marginBottom: '20px' }}>
              Review your journey
            </h3>

            {/* Journey summary */}
            <div className="rounded-xl p-5 mb-5 space-y-4" style={{ background: '#F8F6F2', border: '1px solid var(--border)' }}>
              {[
                { label: 'From', value: originPlace?.name ?? '' },
                { label: 'To', value: destPlace?.name ?? '' },
                { label: 'Departure', value: formatDateTime(departureTime) },
                {
                  label: 'Route',
                  value: routeSegments.length > 0
                    ? `${routeSegments.length} segments · ~${routeTotalMinutes} min`
                    : 'Route unresolved',
                },
                {
                  label: 'Vehicle',
                  value: selectedVehicleMeta && selectedVehicle
                    ? (
                      <span className="inline-flex items-center gap-2" style={{ justifyContent: 'flex-end' }}>
                        <span
                          className="h-7 w-7 rounded-md flex items-center justify-center"
                          style={{ background: '#E8F4ED', border: '1px solid #DCE5DE' }}
                        >
                          <VehicleTypeIcon icon={selectedVehicleMeta.icon} size={16} stroke="#1E6639" />
                        </span>
                        <span>
                          {selectedVehicleMeta.label}
                          {selectedVehicle.isEmergencyVehicle ? ' (Emergency priority)' : ''}
                        </span>
                      </span>
                    )
                    : 'Not selected',
                },
              ].map(({ label, value }) => (
                <div key={label} className="flex justify-between items-start">
                  <span style={{ color: '#4E5953', fontSize: '0.875rem' }}>{label}</span>
                  <span style={{ fontWeight: 600, color: '#1F2421', textAlign: 'right', maxWidth: '220px' }}>{value}</span>
                </div>
              ))}
            </div>

            {segmentFlow.length > 0 && (
              <div className="rounded-xl p-4 mb-5" style={{ background: '#F1FBF4', border: '1px solid #9AD5AF' }}>
                <div className="flex items-center justify-between gap-2 mb-1.5">
                  <h4 style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1E6639', fontSize: '0.9375rem' }}>
                    Segment-by-segment reservation plan
                  </h4>
                  {etaTimeLabel && (
                    <span style={{ color: '#1E6639', fontSize: '0.75rem' }}>
                      ETA {etaTimeLabel}
                    </span>
                  )}
                </div>
                <p style={{ color: '#4E5953', fontSize: '0.75rem', marginBottom: '10px' }}>
                  Capacity is reserved in this exact sequence at submit time.
                </p>
                <div className="space-y-2 max-h-56 overflow-y-auto pr-1">
                  {segmentFlow.map((segment) => {
                    const isBlocked = segment.canReserve === false;
                    const availabilityLabel = segment.availableSlots === undefined
                      ? 'Pending live check'
                      : segment.maxCapacity !== undefined
                        ? `${formatSlots(segment.availableSlots)} / ${formatSlots(segment.maxCapacity)} free`
                        : `${formatSlots(segment.availableSlots)} free`;

                    return (
                      <div
                        key={segment.segmentId}
                        className="rounded-md p-2.5"
                        style={{
                          background: 'white',
                          border: `1px solid ${isBlocked ? '#F5C2BE' : 'var(--border)'}`,
                        }}
                      >
                        <div className="flex items-center justify-between gap-2">
                          <span style={{ color: '#1F2421', fontSize: '0.75rem', fontWeight: 600 }}>
                            {segment.sequence}. {segment.segmentName}
                          </span>
                          <span style={{ color: '#4E5953', fontSize: '0.75rem' }}>
                            {formatClock(segment.timeWindowStart)}-{formatClock(segment.timeWindowEnd)}
                          </span>
                        </div>
                        <div className="flex items-center justify-between gap-2 mt-1">
                          <span style={{ color: '#4E5953', fontSize: '0.75rem' }}>
                            {segment.traversalMinutes} min
                          </span>
                          <span style={{ color: isBlocked ? '#B42318' : '#1E6639', fontSize: '0.75rem', fontWeight: 600 }}>
                            {availabilityLabel}
                          </span>
                        </div>
                      </div>
                    );
                  })}
                </div>
              </div>
            )}

            {/* Route map */}
            <div className="rounded-xl overflow-hidden mb-5" style={{ border: '1px solid var(--border)', height: '280px' }}>
              <OSMMap
                center={mapCenter}
                zoom={originPlace && destPlace ? 7 : 10}
                onReady={handleMapReady}
                style={{ height: '280px' }}
              />
            </div>

            <div className="flex items-start gap-3 p-3.5 rounded-lg mb-6" style={{ background: '#FFF4E0' }}>
              <AlertCircle size={15} color="#A15C00" className="flex-shrink-0 mt-0.5" />
              <p style={{ color: '#7A4500', fontSize: '0.8125rem', lineHeight: 1.55 }}>
                By submitting, you agree that this booking is subject to live road capacity checks. The backend reserves each segment in order using its exact time window. If another driver confirms first, remaining capacity can be exhausted and your booking may be rejected.
              </p>
            </div>

            <div className="flex gap-3">
              <button
                onClick={() => setStep(1)}
                className="px-5 py-2.5 rounded-lg text-sm transition-colors"
                style={{ border: '1.5px solid var(--border)', color: '#4E5953', background: 'white' }}
              >
                Back
              </button>
              <button
                onClick={handleSubmit}
                disabled={submitting}
                className="flex-1 py-2.5 rounded-lg text-white text-sm flex items-center justify-center gap-2 transition-colors"
                style={{ background: submitting ? '#4E5953' : '#2F6B55', fontWeight: 600 }}
              >
                {submitting ? (
                  <>
                    <span className="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin" />
                    Checking road availability...
                  </>
                ) : (
                  'Submit booking'
                )}
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
