import { AlertCircle, ChevronRight, Clock, Info, MapPin } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router';
import { useApp } from '../../context/AppContext';
import { getMapNodes, getRoute, MapNode, MapRoute } from '../../services/mapApi';

const VEHICLE_TYPES = [
  { value: 'Car', label: 'Car', icon: 'CAR', desc: 'Standard passenger vehicle' },
  { value: 'Van', label: 'Van', icon: 'VAN', desc: 'Light commercial vehicle' },
  { value: 'Motorcycle', label: 'Motorcycle', icon: 'MOTO', desc: 'Two-wheeled vehicle' },
  { value: 'HGV', label: 'HGV', icon: 'HGV', desc: 'Heavy goods vehicle' },
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
  mapNodes?: string;
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
  const [mapNodesLoading, setMapNodesLoading] = useState(true);
  const [mapNodesError, setMapNodesError] = useState<string | null>(null);
  const [routePreview, setRoutePreview] = useState<MapRoute | null>(null);
  const [routeLoading, setRouteLoading] = useState(false);
  const [routeError, setRouteError] = useState<string | null>(null);

  const loadMapNodes = () => {
    setMapNodesLoading(true);
    setMapNodesError(null);
    setErrors((current) => ({ ...current, mapNodes: undefined }));
    getMapNodes()
      .then((nodes) => {
        setMapNodes(nodes);
      })
      .catch(() => {
        setMapNodes([]);
        setMapNodesError('Unable to load map nodes. Please retry before booking.');
      })
      .finally(() => {
        setMapNodesLoading(false);
      });
  };

  useEffect(() => {
    loadMapNodes();
  }, []);

  useEffect(() => {
    if (
      step !== 2
      || !form.originNode
      || !form.destNode
      || form.originNode.node_id === form.destNode.node_id
    ) {
      return;
    }

    setRouteLoading(true);
    setRouteError(null);
    getRoute(form.originNode.node_id, form.destNode.node_id)
      .then((route) => {
        setRoutePreview(route);
      })
      .catch((err) => {
        setRoutePreview(null);
        setRouteError(err instanceof Error ? err.message : 'Route preview unavailable.');
      })
      .finally(() => {
        setRouteLoading(false);
      });
  }, [form.destNode, form.originNode, step]);

  const minTime = new Date(Date.now() + 60 * 60 * 1000).toISOString().slice(0, 16);

  const validateStep1 = () => {
    const e: FormErrors = {};
    if (mapNodesError) e.mapNodes = 'Map nodes must load successfully before you can book.';
    if (!form.originNode) e.origin = 'Please select an origin node.';
    if (!form.destNode) e.destination = 'Please select a destination node.';
    if (form.originNode && form.destNode && form.originNode.node_id === form.destNode.node_id) {
      e.destination = 'Origin and destination must be different.';
    }
    if (!form.departureTime) e.departureTime = 'Please choose a departure time.';
    else if (new Date(form.departureTime) < new Date(Date.now() + 55 * 60 * 1000)) {
      e.departureTime = 'Departure must be at least 1 hour from now.';
    }
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
    setRoutePreview(null);
    setRouteError(null);
    setStep(2);
  };

  const handleSubmit = async () => {
    const e = validateStep1();
    if (Object.keys(e).length > 0 || !form.originNode || !form.destNode || !routePreview || routeLoading || routeError) {
      setErrors(e);
      setStep(1);
      return;
    }

    setSubmitting(true);
    try {
      await bookJourney({
        origin: form.originNode.label,
        destination: form.destNode.label,
        originCoords: { lat: form.originNode.lat, lng: form.originNode.lng },
        destCoords: { lat: form.destNode.lat, lng: form.destNode.lng },
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
        weekday: 'short',
        day: 'numeric',
        month: 'short',
        hour: '2-digit',
        minute: '2-digit',
      });
    } catch {
      return dt;
    }
  };

  const nodeLoadMessage = mapNodesError ?? errors.mapNodes;

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
            <div className="flex items-start gap-3 p-3.5 rounded-lg mb-6" style={{ background: '#F0EDE7' }}>
              <Info size={15} color="#4E5953" className="flex-shrink-0 mt-0.5" />
              <p style={{ color: '#4E5953', fontSize: '0.8125rem', lineHeight: 1.55 }}>
                You must book at least <strong style={{ color: '#1F2421' }}>1 hour before departure</strong>. Journeys are checked against live road capacity - peak hours (07:00-09:00) on busy routes may be rejected.
              </p>
            </div>

            <div className="space-y-5">
              {nodeLoadMessage && (
                <div className="rounded-lg p-3.5" style={{ background: '#FEF3F2', border: '1px solid #FECACA' }}>
                  <div className="flex items-start gap-2">
                    <AlertCircle size={15} color="#B42318" className="flex-shrink-0 mt-0.5" />
                    <div className="flex-1">
                      <p style={{ color: '#B42318', fontSize: '0.875rem', fontWeight: 500 }}>
                        {nodeLoadMessage}
                      </p>
                      <button
                        type="button"
                        onClick={loadMapNodes}
                        className="mt-2 text-sm"
                        style={{ color: '#7A271A', fontWeight: 600 }}
                      >
                        Retry loading nodes
                      </button>
                    </div>
                  </div>
                </div>
              )}

              <div>
                <label htmlFor="origin" className="block mb-1.5" style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}>
                  From
                </label>
                <div className="relative">
                  <MapPin size={16} color="#4E5953" className="absolute left-3 top-1/2 -translate-y-1/2 pointer-events-none" />
                  <select
                    id="origin"
                    value={form.originNode?.node_id ?? ''}
                    onChange={(e) => {
                      const node = mapNodes.find((n) => n.node_id === e.target.value);
                      const shouldClearDestination = node?.node_id === form.destNode?.node_id;
                      setForm({
                        ...form,
                        origin: node?.label ?? '',
                        originNode: node,
                        destination: shouldClearDestination ? '' : form.destination,
                        destNode: shouldClearDestination ? undefined : form.destNode,
                      });
                    }}
                    disabled={mapNodesLoading || !!mapNodesError}
                    className="w-full pl-9 pr-4 py-2.5 rounded-lg appearance-none outline-none"
                    style={{
                      border: errors.origin ? '1.5px solid #B42318' : '1.5px solid var(--border)',
                      background: mapNodesLoading || mapNodesError ? '#F8F6F2' : 'white',
                      color: form.originNode ? '#1F2421' : '#4E5953',
                    }}
                  >
                    <option value="">
                      {mapNodesLoading ? 'Loading nodes...' : mapNodesError ? 'Nodes unavailable' : 'Select origin'}
                    </option>
                    {mapNodes.map((n) => (
                      <option key={n.node_id} value={n.node_id}>{n.label}</option>
                    ))}
                  </select>
                </div>
                {errors.origin && (
                  <p className="mt-1.5 text-sm flex items-center gap-1" style={{ color: '#B42318' }}>
                    <AlertCircle size={13} /> {errors.origin}
                  </p>
                )}
              </div>

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
                      setForm({ ...form, destination: node?.label ?? '', destNode: node });
                    }}
                    disabled={mapNodesLoading || !!mapNodesError}
                    className="w-full pl-9 pr-4 py-2.5 rounded-lg appearance-none outline-none"
                    style={{
                      border: errors.destination ? '1.5px solid #B42318' : '1.5px solid var(--border)',
                      background: mapNodesLoading || mapNodesError ? '#F8F6F2' : 'white',
                      color: form.destNode ? '#1F2421' : '#4E5953',
                    }}
                  >
                    <option value="">
                      {mapNodesLoading ? 'Loading nodes...' : mapNodesError ? 'Nodes unavailable' : 'Select destination'}
                    </option>
                    {mapNodes
                      .filter((n) => n.node_id !== form.originNode?.node_id)
                      .map((n) => (
                        <option key={n.node_id} value={n.node_id}>{n.label}</option>
                      ))}
                  </select>
                </div>
                {errors.destination && (
                  <p className="mt-1.5 text-sm flex items-center gap-1" style={{ color: '#B42318' }}>
                    <AlertCircle size={13} /> {errors.destination}
                  </p>
                )}
              </div>

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
                disabled={mapNodesLoading || !!mapNodesError}
                className="flex-1 py-2.5 rounded-lg text-white text-sm transition-colors flex items-center justify-center gap-2"
                style={{
                  background: mapNodesLoading || mapNodesError ? '#4E5953' : '#2F6B55',
                  fontWeight: 600,
                  cursor: mapNodesLoading || mapNodesError ? 'not-allowed' : 'pointer',
                }}
                onMouseEnter={(e) => {
                  if (!mapNodesLoading && !mapNodesError) e.currentTarget.style.background = '#245343';
                }}
                onMouseLeave={(e) => {
                  e.currentTarget.style.background = mapNodesLoading || mapNodesError ? '#4E5953' : '#2F6B55';
                }}
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

            <div className="rounded-xl p-5 mb-5 space-y-4" style={{ background: '#F8F6F2', border: '1px solid var(--border)' }}>
              <div className="flex justify-between items-start">
                <span style={{ color: '#4E5953', fontSize: '0.875rem' }}>From</span>
                <span style={{ fontWeight: 600, color: '#1F2421' }}>{form.originNode?.label ?? form.origin}</span>
              </div>
              <div className="flex justify-between items-start">
                <span style={{ color: '#4E5953', fontSize: '0.875rem' }}>To</span>
                <span style={{ fontWeight: 600, color: '#1F2421' }}>{form.destNode?.label ?? form.destination}</span>
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

            <div className="rounded-xl p-5 mb-5 space-y-4" style={{ background: '#F8F6F2', border: '1px solid var(--border)' }}>
              <div className="flex items-center justify-between">
                <span style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421' }}>
                  Route preview
                </span>
                {routePreview && (
                  <span style={{ color: '#4E5953', fontSize: '0.875rem' }}>
                    {routePreview.total_traversal_time_minutes} min total
                  </span>
                )}
              </div>

              {routeLoading && (
                <p style={{ color: '#4E5953', fontSize: '0.875rem' }}>
                  Loading route preview...
                </p>
              )}

              {routeError && (
                <div className="rounded-lg p-3.5" style={{ background: '#FEF3F2', border: '1px solid #FECACA' }}>
                  <p style={{ color: '#B42318', fontSize: '0.875rem', fontWeight: 500 }}>
                    Route preview unavailable: {routeError}
                  </p>
                  <button
                    type="button"
                    onClick={() => {
                      setRoutePreview(null);
                      setRouteError(null);
                      setStep(1);
                    }}
                    className="mt-2 text-sm"
                    style={{ color: '#7A271A', fontWeight: 600 }}
                  >
                    Back to edit journey
                  </button>
                </div>
              )}

              {!routeLoading && !routeError && routePreview && (
                <div className="space-y-3">
                  {routePreview.segments.map((segment) => (
                    <div
                      key={`${segment.sequence}-${segment.segment_id}`}
                      className="rounded-lg p-3"
                      style={{ background: 'white', border: '1px solid var(--border)' }}
                    >
                      <div className="flex items-center justify-between gap-3">
                        <div>
                          <div style={{ color: '#1F2421', fontWeight: 600, fontSize: '0.875rem' }}>
                            {segment.sequence}. {segment.segment_name}
                          </div>
                          <div style={{ color: '#4E5953', fontSize: '0.75rem' }}>
                            {segment.segment_id}
                          </div>
                        </div>
                        <div style={{ color: '#4E5953', fontSize: '0.8125rem', fontWeight: 500 }}>
                          {segment.traversal_time_minutes} min
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              )}
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
                disabled={submitting || routeLoading || !!routeError || !routePreview}
                className="flex-1 py-2.5 rounded-lg text-white text-sm flex items-center justify-center gap-2 transition-colors"
                style={{
                  background: submitting || routeLoading || routeError || !routePreview ? '#4E5953' : '#2F6B55',
                  fontWeight: 600,
                  cursor: submitting || routeLoading || routeError || !routePreview ? 'not-allowed' : 'pointer',
                }}
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
