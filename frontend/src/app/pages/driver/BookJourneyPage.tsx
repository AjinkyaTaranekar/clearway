import { AlertCircle, ChevronRight, Clock, Info, MapPin } from 'lucide-react';
import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router';
import { useApp } from '../../context/AppContext';
import { getMapNodes, MapNode } from '../../services/mapApi';

const VEHICLE_TYPES = [
  { value: 'Car', label: 'Car', icon: '🚗', desc: 'Standard passenger vehicle' },
  { value: 'Van', label: 'Van', icon: '🚐', desc: 'Light commercial vehicle' },
  { value: 'Motorcycle', label: 'Motorcycle', icon: '🏍️', desc: 'Two-wheeled vehicle' },
  { value: 'HGV', label: 'HGV', icon: '🚛', desc: 'Heavy goods vehicle' },
];

interface FormData {
  origin: string;
  destination: string;
  originNode?: MapNode;
  destNode?: MapNode;
  departureTime: string;
  vehicleType: string;
}

interface FormErrors {
  origin?: string;
  destination?: string;
  departureTime?: string;
  vehicleType?: string;
}

// Map IAM vehicle type values (lowercase) to the booking page display values.
// The booking page uses 'HGV' for what IAM calls 'truck'.
const IAM_TO_BOOKING_VEHICLE: Record<string, string> = {
  car: 'Car',
  van: 'Van',
  motorcycle: 'Motorcycle',
  truck: 'HGV',
};

export default function BookJourneyPage() {
  const navigate = useNavigate();
  const { bookJourney, user } = useApp();

  const defaultVehicleType = user?.vehicle_type
    ? (IAM_TO_BOOKING_VEHICLE[user.vehicle_type.toLowerCase()] ?? '')
    : '';

  const [step, setStep] = useState(1);
  const [form, setForm] = useState<FormData>({
    origin: '',
    destination: '',
    departureTime: '',
    vehicleType: defaultVehicleType,
  });
  const [errors, setErrors] = useState<FormErrors>({});
  const [submitting, setSubmitting] = useState(false);
  const [mapNodes, setMapNodes] = useState<MapNode[]>([]);

  useEffect(() => {
    getMapNodes()
      .then(setMapNodes)
      .catch(() => {}); // fall back to empty — selects will just be empty
  }, []);

  // Min time = now + 60 min
  const minTime = new Date(Date.now() + 60 * 60 * 1000).toISOString().slice(0, 16);

  const validateStep1 = () => {
    const e: FormErrors = {};
    if (!form.origin) e.origin = 'Please select an origin.';
    if (!form.destination) e.destination = 'Please select a destination.';
    if (form.origin && form.destination && form.origin === form.destination)
      e.destination = 'Origin and destination must be different.';
    if (!form.departureTime) e.departureTime = 'Please choose a departure time.';
    else if (new Date(form.departureTime) < new Date(Date.now() + 55 * 60 * 1000))
      e.departureTime = 'Departure must be at least 1 hour from now.';
    if (!form.vehicleType) e.vehicleType = 'Please select a vehicle type.';
    return e;
  };

  const handleNext = () => {
    const e = validateStep1();
    if (Object.keys(e).length > 0) {
      setErrors(e);
      return;
    }
    setErrors({});
    setStep(2);
  };

  const handleSubmit = async () => {
    setSubmitting(true);
    try {
      await bookJourney({
        origin: form.origin,
        destination: form.destination,
        originCoords: form.originNode ? { lat: form.originNode.lat, lng: form.originNode.lng } : undefined,
        destCoords: form.destNode ? { lat: form.destNode.lat, lng: form.destNode.lng } : undefined,
        departureTime: form.departureTime,
        vehicleType: form.vehicleType,
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
      const d = new Date(dt);
      return d.toLocaleString('en-GB', {
        weekday: 'short', day: 'numeric', month: 'short',
        hour: '2-digit', minute: '2-digit',
      });
    } catch { return dt; }
  };

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

      {/* Stepper */}
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
            <span
              style={{
                fontSize: '0.875rem',
                fontWeight: step === s ? 600 : 400,
                color: step === s ? '#1F2421' : '#4E5953',
              }}
            >
              {s === 1 ? 'Journey details' : 'Review & submit'}
            </span>
            {s < 2 && <ChevronRight size={14} color="#4E5953" />}
          </div>
        ))}
      </div>

      <div className="bg-white rounded-2xl p-6" style={{ border: '1px solid var(--border)' }}>
        {step === 1 ? (
          <div>
            {/* Policy hint */}
            <div
              className="flex items-start gap-3 p-3.5 rounded-lg mb-6"
              style={{ background: '#F0EDE7' }}
            >
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
                <div className="relative">
                  <MapPin size={16} color="#4E5953" className="absolute left-3 top-1/2 -translate-y-1/2 pointer-events-none" />
                  <select
                    id="origin"
                    value={form.origin}
                    onChange={(e) => {
                      const node = mapNodes.find((n) => n.node_id === e.target.value);
                      setForm({ ...form, origin: node?.label ?? e.target.value, originNode: node });
                    }}
                    className="w-full pl-9 pr-4 py-2.5 rounded-lg appearance-none outline-none"
                    style={{
                      border: errors.origin ? '1.5px solid #B42318' : '1.5px solid var(--border)',
                      background: 'white',
                      color: form.origin ? '#1F2421' : '#4E5953',
                    }}
                  >
                    <option value="">Select origin</option>
                    {mapNodes.map((n) => <option key={n.node_id} value={n.node_id}>{n.label}</option>)}
                  </select>
                </div>
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
                <div className="relative">
                  <MapPin size={16} color="#B65C3A" className="absolute left-3 top-1/2 -translate-y-1/2 pointer-events-none" />
                  <select
                    id="destination"
                    value={form.destNode?.node_id ?? ''}
                    onChange={(e) => {
                      const node = mapNodes.find((n) => n.node_id === e.target.value);
                      setForm({ ...form, destination: node?.label ?? e.target.value, destNode: node });
                    }}
                    className="w-full pl-9 pr-4 py-2.5 rounded-lg appearance-none outline-none"
                    style={{
                      border: errors.destination ? '1.5px solid #B42318' : '1.5px solid var(--border)',
                      background: 'white',
                      color: form.destination ? '#1F2421' : '#4E5953',
                    }}
                  >
                    <option value="">Select destination</option>
                    {mapNodes
                      .filter((n) => n.node_id !== form.originNode?.node_id)
                      .map((n) => <option key={n.node_id} value={n.node_id}>{n.label}</option>)}
                  </select>
                </div>
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
                    value={form.departureTime}
                    min={minTime}
                    onChange={(e) => setForm({ ...form, departureTime: e.target.value })}
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
                      onClick={() => setForm({ ...form, vehicleType: v.value })}
                      className="flex items-center gap-3 p-3 rounded-xl text-left transition-all"
                      style={{
                        border: form.vehicleType === v.value
                          ? '1.5px solid #2F6B55'
                          : errors.vehicleType ? '1.5px solid #B42318' : '1.5px solid var(--border)',
                        background: form.vehicleType === v.value ? '#E8F4ED' : 'white',
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
                onMouseEnter={(e) => (e.currentTarget.style.background = '#245343')}
                onMouseLeave={(e) => (e.currentTarget.style.background = '#2F6B55')}
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

            <div
              className="rounded-xl p-5 mb-5 space-y-4"
              style={{ background: '#F8F6F2', border: '1px solid var(--border)' }}
            >
              <div className="flex justify-between items-start">
                <span style={{ color: '#4E5953', fontSize: '0.875rem' }}>From</span>
                <span style={{ fontWeight: 600, color: '#1F2421' }}>{form.origin}</span>
              </div>
              <div className="flex justify-between items-start">
                <span style={{ color: '#4E5953', fontSize: '0.875rem' }}>To</span>
                <span style={{ fontWeight: 600, color: '#1F2421' }}>{form.destination}</span>
              </div>
              <div className="flex justify-between items-start">
                <span style={{ color: '#4E5953', fontSize: '0.875rem' }}>Departure</span>
                <span style={{ fontWeight: 600, color: '#1F2421', textAlign: 'right', maxWidth: '200px' }}>
                  {formatDateTime(form.departureTime)}
                </span>
              </div>
              <div className="flex justify-between items-start">
                <span style={{ color: '#4E5953', fontSize: '0.875rem' }}>Vehicle</span>
                <span style={{ fontWeight: 600, color: '#1F2421' }}>
                  {VEHICLE_TYPES.find((v) => v.value === form.vehicleType)?.icon}{' '}
                  {form.vehicleType}
                </span>
              </div>
            </div>

            <div
              className="flex items-start gap-3 p-3.5 rounded-lg mb-6"
              style={{ background: '#FFF4E0' }}
            >
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
                style={{
                  background: submitting ? '#4E5953' : '#2F6B55',
                  fontWeight: 600,
                  cursor: submitting ? 'not-allowed' : 'pointer',
                }}
              >
                {submitting ? (
                  <>
                    <span className="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin" />
                    Checking road availability…
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
