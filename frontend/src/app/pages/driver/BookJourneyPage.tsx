import { LngLatBounds, Map as MapLibreMap } from 'maplibre-gl';
import { AlertCircle, ChevronRight, Clock, Info } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router';
import PlaceSearch from '../../components/ui/PlaceSearch';
import TomTomMap, { addMarker, addPolyline } from '../../components/ui/TomTomMap';
import { useApp } from '../../context/AppContext';
import { PlaceResult } from '../../services/mapApi';

const VEHICLE_TYPES = [
  { value: 'Car', label: 'Car', icon: 'CAR', desc: 'Standard passenger vehicle' },
  { value: 'Van', label: 'Van', icon: 'VAN', desc: 'Light commercial vehicle' },
  { value: 'Motorcycle', label: 'Motorcycle', icon: 'MOTO', desc: 'Two-wheeled vehicle' },
  { value: 'HGV', label: 'HGV', icon: 'HGV', desc: 'Heavy goods vehicle' },
];

// Map IAM vehicle type values (lowercase) to the booking page display values.
const IAM_TO_BOOKING_VEHICLE: Record<string, string> = {
  car: 'Car',
  van: 'Van',
  motorcycle: 'Motorcycle',
  truck: 'HGV',
};

// Default map centre: Dublin, Ireland
const DUBLIN: [number, number] = [53.3498, -6.2603];

interface FormErrors {
  origin?: string;
  destination?: string;
  departureTime?: string;
  vehicleType?: string;
}

export default function BookJourneyPage() {
  const navigate = useNavigate();
  const { bookJourney, user } = useApp();

  const defaultVehicleType = user?.vehicle_type
    ? (IAM_TO_BOOKING_VEHICLE[user.vehicle_type.toLowerCase()] ?? '')
    : '';

  const [step, setStep] = useState(1);
  const [originPlace, setOriginPlace] = useState<PlaceResult | null>(null);
  const [destPlace, setDestPlace] = useState<PlaceResult | null>(null);
  const [departureTime, setDepartureTime] = useState('');
  const [vehicleType, setVehicleType] = useState(defaultVehicleType);
  const [errors, setErrors] = useState<FormErrors>({});
  const [submitting, setSubmitting] = useState(false);

  const mapRef = useRef<MapLibreMap | null>(null);

  const minTime = new Date(Date.now() + 60 * 60 * 1000).toISOString().slice(0, 16);

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
    }
    if (!vehicleType) e.vehicleType = 'Please select a vehicle type.';
    return e;
  };

  const handleNext = () => {
    const e = validateStep1();
    if (Object.keys(e).length > 0) { setErrors(e); return; }
    setErrors({});
    setStep(2);
  };

  // Draw route preview on step 2 once map is ready
  const handleMapReady = (map: tt.Map) => {
    mapRef.current = map;
    drawRouteOverlay(map);
  };

  // Re-draw if places change while on step 2
  useEffect(() => {
    if (step !== 2 || !mapRef.current) return;
    drawRouteOverlay(mapRef.current);
  }, [step, originPlace, destPlace]);

  const drawRouteOverlay = (map: tt.Map) => {
    if (!originPlace || !destPlace) return;
    // Draw a direct line between O and D — simple preview without calling TomTom routing
    addPolyline(
      map,
      'route-preview',
      [[originPlace.lat, originPlace.lng], [destPlace.lat, destPlace.lng]],
      '#2F6B55',
      4,
    );
    addMarker(map, originPlace.lat, originPlace.lng, '#2F6B55');
    addMarker(map, destPlace.lat, destPlace.lng, '#B65C3A');

    // Fit map to show both points
    const bounds = new LngLatBounds(
      [originPlace.lng, originPlace.lat],
      [destPlace.lng, destPlace.lat],
    );
    map.fitBounds(bounds, { padding: 60, maxZoom: 13 });
  };

  const handleSubmit = async () => {
    const e = validateStep1();
    if (Object.keys(e).length > 0 || !originPlace || !destPlace) {
      setErrors(e);
      setStep(1);
      return;
    }
    setSubmitting(true);
    try {
      await bookJourney({
        origin: originPlace.name,
        destination: destPlace.name,
        originCoords: { lat: originPlace.lat, lng: originPlace.lng },
        destCoords: { lat: destPlace.lat, lng: destPlace.lng },
        departureTime,
        vehicleType,
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

  // Map centre: midpoint between O and D, or Dublin default
  const mapCenter: [number, number] =
    originPlace && destPlace
      ? [(originPlace.lat + destPlace.lat) / 2, (originPlace.lng + destPlace.lng) / 2]
      : originPlace
        ? [originPlace.lat, originPlace.lng]
        : DUBLIN;

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
                You must book at least <strong style={{ color: '#1F2421' }}>1 hour before departure</strong>. Journeys are checked against live road capacity — peak hours (07:00–09:00) on busy routes may be rejected.
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
                  placeholder="Search origin (e.g. Dublin, Cork…)"
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
                  placeholder="Search destination (e.g. Galway, Limerick…)"
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

              {/* Departure time */}
              <div>
                <label htmlFor="departure" className="block mb-1.5" style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>
                  Departure time
                </label>
                <div className="relative">
                  <Clock size={16} color="#4E5953" className="absolute left-3 top-1/2 -translate-y-1/2 pointer-events-none" />
                  <input
                    id="departure"
                    type="datetime-local"
                    value={departureTime}
                    min={minTime}
                    onChange={(e) => setDepartureTime(e.target.value)}
                    className="w-full pl-9 pr-4 py-2.5 rounded-lg outline-none"
                    style={{
                      border: errors.departureTime ? '1.5px solid #B42318' : '1.5px solid var(--border)',
                      background: 'white',
                      color: '#1F2421',
                    }}
                  />
                </div>
                {errors.departureTime && (
                  <p className="mt-1.5 text-sm flex items-center gap-1" style={{ color: '#B42318' }}>
                    <AlertCircle size={13} /> {errors.departureTime}
                  </p>
                )}
              </div>

              {/* Vehicle type */}
              <div>
                <label className="block mb-2" style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>
                  Vehicle type
                </label>
                {errors.vehicleType && (
                  <p className="mb-2 text-sm flex items-center gap-1" style={{ color: '#B42318' }}>
                    <AlertCircle size={13} /> {errors.vehicleType}
                  </p>
                )}
                <div className="grid grid-cols-2 gap-2">
                  {VEHICLE_TYPES.map((v) => (
                    <button
                      key={v.value}
                      type="button"
                      onClick={() => setVehicleType(v.value)}
                      className="flex items-center gap-3 p-3 rounded-xl text-left transition-all"
                      style={{
                        border: vehicleType === v.value
                          ? '1.5px solid #2F6B55'
                          : errors.vehicleType ? '1.5px solid #B42318' : '1.5px solid var(--border)',
                        background: vehicleType === v.value ? '#E8F4ED' : 'white',
                      }}
                    >
                      <span className="text-xl">{v.icon}</span>
                      <div>
                        <div style={{ fontWeight: 600, color: '#1F2421', fontSize: '0.875rem', fontFamily: 'var(--font-heading)' }}>
                          {v.label}
                        </div>
                        <div style={{ color: '#4E5953', fontSize: '0.75rem' }}>{v.desc}</div>
                      </div>
                    </button>
                  ))}
                </div>
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
                  label: 'Vehicle',
                  value: `${VEHICLE_TYPES.find((v) => v.value === vehicleType)?.icon ?? ''} ${vehicleType}`,
                },
              ].map(({ label, value }) => (
                <div key={label} className="flex justify-between items-start">
                  <span style={{ color: '#4E5953', fontSize: '0.875rem' }}>{label}</span>
                  <span style={{ fontWeight: 600, color: '#1F2421', textAlign: 'right', maxWidth: '220px' }}>{value}</span>
                </div>
              ))}
            </div>

            {/* TomTom route map */}
            <div className="rounded-xl overflow-hidden mb-5" style={{ border: '1px solid var(--border)', height: '280px' }}>
              <TomTomMap
                center={mapCenter}
                zoom={originPlace && destPlace ? 7 : 10}
                onReady={handleMapReady}
                style={{ height: '280px' }}
              />
            </div>

            <div className="flex items-start gap-3 p-3.5 rounded-lg mb-6" style={{ background: '#FFF4E0' }}>
              <AlertCircle size={15} color="#A15C00" className="flex-shrink-0 mt-0.5" />
              <p style={{ color: '#7A4500', fontSize: '0.8125rem', lineHeight: 1.55 }}>
                By submitting, you agree that this booking is subject to live road capacity checks. The system may reject the booking if a segment is full at your chosen time.
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
