import { useState } from 'react';
import { useNavigate, useParams } from 'react-router';
import { useApp } from '../../context/AppContext';
import { StatusChip } from '../../components/ui/StatusChip';
import {
  ArrowLeft, CheckCircle2, XCircle, AlertCircle, Play, Clock,
  User, Car, MapPin, Shield,
} from 'lucide-react';
import { format, parseISO } from 'date-fns';

export default function AdminJourneyDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { adminJourneys, updateJourneyStatus } = useApp();

  const [showForceCancel, setShowForceCancel] = useState(false);
  const [actionLoading, setActionLoading] = useState(false);
  const [cancelled, setCancelled] = useState(false);

  const journey = adminJourneys.find((j) => j.id === id);

  if (!journey) {
    return (
      <div className="p-8 text-center max-w-xl mx-auto">
        <div className="w-14 h-14 rounded-full flex items-center justify-center mx-auto mb-4" style={{ background: '#F0EDE7' }}>
          <AlertCircle size={24} color="#4E5953" />
        </div>
        <p style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', marginBottom: '8px' }}>Journey not found</p>
        <p style={{ color: '#4E5953', fontSize: '0.9375rem', marginBottom: '20px' }}>
          This journey may have been removed or the ID is incorrect.
        </p>
        <button
          onClick={() => navigate('/admin/journeys')}
          className="px-5 py-2.5 rounded-lg text-white text-sm"
          style={{ background: '#2F6B55', fontWeight: 600 }}
        >
          Back to all journeys
        </button>
      </div>
    );
  }

  const canForceCancel = journey.status === 'approved' || journey.status === 'active' || journey.status === 'pending';

  const formatDT = (ts: string) => {
    try { return format(parseISO(ts), 'EEE, d MMM yyyy · HH:mm'); } catch { return ts; }
  };
  const formatTime = (ts: string) => {
    try { return format(parseISO(ts), 'HH:mm'); } catch { return ts; }
  };
  const formatShort = (ts: string) => {
    try { return format(parseISO(ts), 'd MMM · HH:mm'); } catch { return ts; }
  };

  const occupancyColor = (level: string) => {
    if (level === 'low') return '#2E7D32';
    if (level === 'medium') return '#A15C00';
    if (level === 'high') return '#C65D3A';
    return '#A61E1E';
  };

  const timelineIconColor = (type: string) => {
    if (type === 'approved' || type === 'completed') return '#2E7D32';
    if (type === 'rejected' || type === 'cancelled') return '#B42318';
    if (type === 'active') return '#1A4E80';
    return '#4E5953';
  };

  const vehicleIcons: Record<string, string> = {
    Car: '🚗', Van: '🚐', Motorcycle: '🏍️', HGV: '🚛',
  };

  const handleForceCancel = async () => {
    setActionLoading(true);
    await updateJourneyStatus(journey.id, 'cancelled', 'Admin');
    setCancelled(true);
    setActionLoading(false);
    setShowForceCancel(false);
  };

  return (
    <div className="p-5 lg:p-8 max-w-2xl mx-auto">
      {/* Back */}
      <button
        onClick={() => navigate('/admin/journeys')}
        className="flex items-center gap-2 mb-5 text-sm hover:opacity-70 transition-opacity"
        style={{ color: '#4E5953' }}
      >
        <ArrowLeft size={16} />
        Back to all journeys
      </button>

      {/* Admin badge */}
      <div
        className="flex items-center gap-2 px-3 py-1.5 rounded-lg mb-5 w-fit"
        style={{ background: '#E3EEFB', border: '1px solid #B3D0F5' }}
      >
        <Shield size={13} color="#1A4E80" />
        <span style={{ color: '#1A4E80', fontSize: '0.8125rem', fontWeight: 600 }}>Admin view</span>
      </div>

      {/* Header */}
      <div className="flex items-start justify-between gap-3 mb-5">
        <div>
          <div className="flex items-center gap-2 mb-1.5">
            <StatusChip status={cancelled ? 'cancelled' : journey.status} />
            <span style={{ color: '#4E5953', fontSize: '0.875rem' }}>{journey.id}</span>
          </div>
          <h1 style={{ fontFamily: 'var(--font-heading)', fontWeight: 700, color: '#1F2421', fontSize: '1.375rem', lineHeight: 1.25 }}>
            {journey.origin} → {journey.destination}
          </h1>
        </div>
      </div>

      {/* Rejection notice */}
      {journey.status === 'rejected' && journey.rejectionReason && (
        <div
          className="flex items-start gap-3 p-4 rounded-xl mb-5"
          style={{ background: '#FDECEA', border: '1px solid #F5C2BE' }}
        >
          <XCircle size={18} color="#B42318" className="flex-shrink-0 mt-0.5" />
          <div>
            <p style={{ fontWeight: 600, color: '#8E1B13', fontSize: '0.9375rem', marginBottom: '4px' }}>Why this was rejected</p>
            <p style={{ color: '#8E1B13', fontSize: '0.875rem', lineHeight: 1.65 }}>{journey.rejectionReason}</p>
          </div>
        </div>
      )}

      {/* Driver info */}
      <div className="bg-white rounded-xl p-5 mb-4" style={{ border: '1px solid var(--border)' }}>
        <div className="flex items-center gap-2.5 mb-4">
          <User size={16} color="#2F6B55" />
          <h4 style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem' }}>
            Driver information
          </h4>
        </div>
        <div className="grid grid-cols-2 gap-4">
          <div>
            <div style={{ color: '#4E5953', fontSize: '0.75rem', marginBottom: '2px' }}>Name</div>
            <div style={{ fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem' }}>{journey.driverName}</div>
          </div>
          <div>
            <div style={{ color: '#4E5953', fontSize: '0.75rem', marginBottom: '2px' }}>Driver ID</div>
            <div style={{ fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem' }}>{journey.driverId}</div>
          </div>
          <div>
            <div style={{ color: '#4E5953', fontSize: '0.75rem', marginBottom: '2px' }}>Vehicle</div>
            <div style={{ fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem' }}>
              {vehicleIcons[journey.vehicleType]} {journey.vehicleType}
            </div>
          </div>
          <div>
            <div style={{ color: '#4E5953', fontSize: '0.75rem', marginBottom: '2px' }}>Region</div>
            <div style={{ fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem' }}>{journey.region}</div>
          </div>
        </div>
      </div>

      {/* Journey details */}
      <div className="bg-white rounded-xl p-5 mb-4" style={{ border: '1px solid var(--border)' }}>
        <div className="flex items-center gap-2.5 mb-4">
          <MapPin size={16} color="#2F6B55" />
          <h4 style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem' }}>
            Journey details
          </h4>
        </div>
        <div className="flex items-center gap-3 mb-4">
          <div className="flex-1">
            <div className="flex items-center gap-2 mb-1.5">
              <div className="w-2.5 h-2.5 rounded-full" style={{ background: '#2F6B55' }} />
              <div>
                <div style={{ fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem' }}>{journey.origin}</div>
                <div style={{ color: '#4E5953', fontSize: '0.8125rem' }}>Departure</div>
              </div>
            </div>
            <div className="ml-[5px] border-l-2 border-dashed h-6" style={{ borderColor: 'var(--border)' }} />
            <div className="flex items-center gap-2">
              <div className="w-2.5 h-2.5 rounded-full" style={{ background: '#B65C3A' }} />
              <div>
                <div style={{ fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem' }}>{journey.destination}</div>
                <div style={{ color: '#4E5953', fontSize: '0.8125rem' }}>Destination</div>
              </div>
            </div>
          </div>
          <div className="text-right">
            <div style={{ fontFamily: 'var(--font-heading)', fontWeight: 700, fontSize: '1.375rem', color: '#1F2421', lineHeight: 1.1 }}>
              {formatTime(journey.departureTime)}
            </div>
            <div style={{ color: '#4E5953', fontSize: '0.8125rem' }}>
              {format(parseISO(journey.departureTime), 'EEE, d MMM')}
            </div>
          </div>
        </div>
        <div className="grid grid-cols-3 gap-3 pt-4" style={{ borderTop: '1px solid var(--border)' }}>
          <div className="text-center">
            <div style={{ color: '#4E5953', fontSize: '0.75rem', marginBottom: '2px' }}>Distance</div>
            <div style={{ fontWeight: 600, color: '#1F2421' }}>{journey.distance}</div>
          </div>
          <div className="text-center" style={{ borderLeft: '1px solid var(--border)', borderRight: '1px solid var(--border)' }}>
            <div style={{ color: '#4E5953', fontSize: '0.75rem', marginBottom: '2px' }}>Duration</div>
            <div style={{ fontWeight: 600, color: '#1F2421' }}>{journey.duration}</div>
          </div>
          <div className="text-center">
            <div style={{ color: '#4E5953', fontSize: '0.75rem', marginBottom: '2px' }}>Booked</div>
            <div style={{ fontWeight: 600, color: '#1F2421', fontSize: '0.8125rem' }}>{formatShort(journey.createdAt)}</div>
          </div>
        </div>
      </div>

      {/* Route segments */}
      {journey.segments.length > 0 && (
        <div className="bg-white rounded-xl p-5 mb-4" style={{ border: '1px solid var(--border)' }}>
          <h4 style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', marginBottom: '14px', fontSize: '0.9375rem' }}>
            Route segments
          </h4>
          <div className="space-y-2.5">
            {journey.segments.map((seg) => (
              <div key={seg.id} className="flex items-center justify-between p-3 rounded-lg" style={{ background: '#F8F6F2' }}>
                <div className="flex items-center gap-2.5">
                  <div className="w-2.5 h-2.5 rounded-full" style={{ background: occupancyColor(seg.level) }} />
                  <span style={{ color: '#1F2421', fontWeight: 500, fontSize: '0.875rem' }}>{seg.name}</span>
                </div>
                <div className="flex items-center gap-3">
                  <div className="w-20 h-1.5 rounded-full overflow-hidden" style={{ background: 'var(--border)' }}>
                    <div className="h-full rounded-full" style={{ width: `${seg.occupancy}%`, background: occupancyColor(seg.level) }} />
                  </div>
                  <span style={{ color: '#4E5953', fontSize: '0.8125rem', minWidth: '32px', textAlign: 'right' }}>
                    {seg.occupancy}%
                  </span>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Timeline */}
      <div className="bg-white rounded-xl p-5 mb-5" style={{ border: '1px solid var(--border)' }}>
        <h4 style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', marginBottom: '14px', fontSize: '0.9375rem' }}>
          Lifecycle history
        </h4>
        <ol>
          {(cancelled
            ? [...journey.timeline, { id: 'TC', type: 'cancelled', label: 'Force cancelled by admin', timestamp: new Date().toISOString(), by: 'Admin' }]
            : journey.timeline
          ).map((event, i, arr) => (
            <li key={event.id} className="flex items-start gap-3 pb-4">
              <div className="flex flex-col items-center">
                <div
                  className="w-7 h-7 rounded-full flex items-center justify-center flex-shrink-0"
                  style={{ background: `${timelineIconColor(event.type)}18`, border: `2px solid ${timelineIconColor(event.type)}` }}
                >
                  {event.type === 'approved' || event.type === 'completed' ? (
                    <CheckCircle2 size={13} color={timelineIconColor(event.type)} />
                  ) : event.type === 'rejected' || event.type === 'cancelled' ? (
                    <XCircle size={13} color={timelineIconColor(event.type)} />
                  ) : event.type === 'active' ? (
                    <Play size={13} color={timelineIconColor(event.type)} />
                  ) : (
                    <Clock size={13} color={timelineIconColor(event.type)} />
                  )}
                </div>
                {i < arr.length - 1 && (
                  <div className="w-px flex-1 mt-1" style={{ background: 'var(--border)', minHeight: '12px' }} />
                )}
              </div>
              <div className="pb-1">
                <div style={{ fontWeight: 600, color: '#1F2421', fontSize: '0.875rem' }}>{event.label}</div>
                <div style={{ color: '#4E5953', fontSize: '0.8125rem' }}>
                  {formatShort(event.timestamp)} · {event.by}
                </div>
              </div>
            </li>
          ))}
        </ol>
      </div>

      {/* Admin Force-Cancel */}
      {canForceCancel && !cancelled && (
        <div className="bg-white rounded-xl p-5" style={{ border: '1.5px solid #F0D8A0' }}>
          <div className="flex items-center gap-2.5 mb-3">
            <Shield size={16} color="#A15C00" />
            <h4 style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem' }}>
              Admin action
            </h4>
          </div>

          {!showForceCancel ? (
            <>
              <p style={{ color: '#4E5953', fontSize: '0.875rem', lineHeight: 1.6, marginBottom: '16px' }}>
                As an admin, you can force-cancel this journey. This will immediately release the road capacity reservation and notify the driver.
              </p>
              <button
                onClick={() => setShowForceCancel(true)}
                className="flex items-center gap-2 px-5 py-2.5 rounded-lg text-sm transition-colors"
                style={{ border: '1.5px solid #F5C2BE', color: '#B42318', background: 'white', fontWeight: 600 }}
                onMouseEnter={(e) => (e.currentTarget.style.background = '#FDECEA')}
                onMouseLeave={(e) => (e.currentTarget.style.background = 'white')}
              >
                <XCircle size={15} /> Force cancel journey
              </button>
            </>
          ) : (
            <div>
              <div className="flex items-start gap-3 p-4 rounded-lg mb-4" style={{ background: '#FDECEA' }}>
                <AlertCircle size={16} color="#B42318" className="flex-shrink-0 mt-0.5" />
                <div>
                  <p style={{ fontWeight: 600, color: '#8E1B13', fontSize: '0.875rem', marginBottom: '4px' }}>
                    Confirm force cancellation
                  </p>
                  <p style={{ color: '#8E1B13', fontSize: '0.875rem', lineHeight: 1.65 }}>
                    This will immediately cancel journey {journey.id} for {journey.driverName}. The driver will be notified and road capacity will be released. This action cannot be undone.
                  </p>
                </div>
              </div>
              <div className="flex gap-3">
                <button
                  onClick={() => setShowForceCancel(false)}
                  disabled={actionLoading}
                  className="flex-1 py-2.5 rounded-lg text-sm transition-colors"
                  style={{ border: '1.5px solid var(--border)', color: '#4E5953', background: 'white', fontWeight: 500 }}
                >
                  Keep journey active
                </button>
                <button
                  onClick={handleForceCancel}
                  disabled={actionLoading}
                  className="flex-1 py-2.5 rounded-lg text-white text-sm flex items-center justify-center gap-2 transition-colors"
                  style={{ background: '#B42318', fontWeight: 600, cursor: actionLoading ? 'not-allowed' : 'pointer' }}
                >
                  {actionLoading ? (
                    <span className="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin" />
                  ) : (
                    <XCircle size={15} />
                  )}
                  Confirm force cancel
                </button>
              </div>
            </div>
          )}
        </div>
      )}

      {/* Post-cancel confirmation */}
      {cancelled && (
        <div className="flex items-start gap-3 p-4 rounded-xl" style={{ background: '#F0EDE7', border: '1px solid var(--border)' }}>
          <CheckCircle2 size={18} color="#2E7D32" className="flex-shrink-0 mt-0.5" />
          <div>
            <p style={{ fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem', marginBottom: '4px' }}>Journey force-cancelled</p>
            <p style={{ color: '#4E5953', fontSize: '0.875rem' }}>
              The journey has been cancelled successfully. Road capacity has been released and the driver has been notified.
            </p>
          </div>
        </div>
      )}
    </div>
  );
}
