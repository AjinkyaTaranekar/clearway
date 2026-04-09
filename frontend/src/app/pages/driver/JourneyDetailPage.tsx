import { useState } from 'react';
import { useNavigate, useParams } from 'react-router';
import { useApp } from '../../context/AppContext';
import JourneyRouteMapCard from '../../components/journey/JourneyRouteMapCard';
import { StatusChip } from '../../components/ui/StatusChip';
import {
  ArrowLeft, CheckCircle2, XCircle, AlertCircle, Play,
  Clock,
} from 'lucide-react';
import { format, parseISO } from 'date-fns';

export default function JourneyDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { journeys, updateJourneyStatus, user } = useApp();
  const [showCancelConfirm, setShowCancelConfirm] = useState(false);
  const [actionLoading, setActionLoading] = useState<string | null>(null);

  const journey = journeys.find((j) => j.id === id);

  if (!journey) {
    return (
      <div className="p-8 text-center">
        <p style={{ color: '#4E5953' }}>Journey not found.</p>
        <button
          onClick={() => navigate('/driver/journeys')}
          className="mt-4 px-5 py-2.5 rounded-lg text-white text-sm"
          style={{ background: '#2F6B55', fontWeight: 600 }}
        >
          Back to journeys
        </button>
      </div>
    );
  }

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

  const handleAction = async (action: 'active' | 'completed' | 'cancelled') => {
    setActionLoading(action);
    await updateJourneyStatus(journey.id, action);
    setActionLoading(null);
    setShowCancelConfirm(false);
  };

  const canActivate = journey.status === 'approved';
  const canComplete = journey.status === 'active';
  const canCancel = journey.status === 'approved' || journey.status === 'pending';

  return (
    <div className="p-5 lg:p-8 max-w-2xl mx-auto">
      {/* Back */}
      <button
        onClick={() => navigate(user?.role === 'admin' ? '/admin/journeys' : '/driver/journeys')}
        className="flex items-center gap-2 mb-5 text-sm hover:opacity-70 transition-opacity"
        style={{ color: '#4E5953' }}
      >
        <ArrowLeft size={16} />
        Back to journeys
      </button>

      {/* Header */}
      <div className="flex items-start justify-between gap-3 mb-5">
        <div>
          <div className="flex items-center gap-2 mb-1.5">
            <StatusChip status={journey.status} />
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
            <p style={{ fontWeight: 600, color: '#8E1B13', fontSize: '0.9375rem', marginBottom: '4px' }}>
              Why this journey was rejected
            </p>
            <p style={{ color: '#8E1B13', fontSize: '0.875rem', lineHeight: 1.65 }}>
              {journey.rejectionReason}
            </p>
          </div>
        </div>
      )}

      {/* Journey details */}
      <div className="bg-white rounded-xl p-5 mb-4" style={{ border: '1px solid var(--border)' }}>
        <h4 style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', marginBottom: '16px' }}>
          Journey details
        </h4>

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
                <div style={{ color: '#4E5953', fontSize: '0.8125rem' }}>Arrival</div>
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
            <div style={{ color: '#4E5953', fontSize: '0.75rem', marginBottom: '2px' }}>Vehicle</div>
            <div style={{ fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem' }}>
              {journey.vehicleType}
            </div>
          </div>
          <div className="text-center" style={{ borderLeft: '1px solid var(--border)', borderRight: '1px solid var(--border)' }}>
            <div style={{ color: '#4E5953', fontSize: '0.75rem', marginBottom: '2px' }}>Distance</div>
            <div style={{ fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem' }}>{journey.distance}</div>
          </div>
          <div className="text-center">
            <div style={{ color: '#4E5953', fontSize: '0.75rem', marginBottom: '2px' }}>Duration</div>
            <div style={{ fontWeight: 600, color: '#1F2421', fontSize: '0.9375rem' }}>{journey.duration}</div>
          </div>
        </div>
      </div>

      <JourneyRouteMapCard journey={journey} title="Journey route map" />

      {/* Route segments */}
      {journey.segments.length > 0 && (
        <div className="bg-white rounded-xl p-5 mb-4" style={{ border: '1px solid var(--border)' }}>
          <h4 style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', marginBottom: '14px' }}>
            Route segments
          </h4>
          <div className="space-y-2.5">
            {journey.segments.map((seg, index) => (
              <div
                key={seg.id}
                className="flex items-center justify-between p-3 rounded-lg"
                style={{ background: '#F8F6F2' }}
              >
                <div className="flex items-center gap-2.5">
                  <div
                    className="w-2.5 h-2.5 rounded-full flex-shrink-0"
                    style={{ background: occupancyColor(seg.level) }}
                  />
                  <div>
                    <div style={{ color: '#1F2421', fontWeight: 500, fontSize: '0.875rem' }}>
                      {seg.sequenceOrder ?? index + 1}. {seg.name}
                    </div>
                    {(seg.timeWindowStart || seg.traversalMinutes) && (
                      <div style={{ color: '#4E5953', fontSize: '0.75rem' }}>
                        {seg.timeWindowStart && seg.timeWindowEnd
                          ? `${formatTime(seg.timeWindowStart)}-${formatTime(seg.timeWindowEnd)}`
                          : 'Window unavailable'}
                        {seg.traversalMinutes ? ` · ${seg.traversalMinutes} min` : ''}
                      </div>
                    )}
                  </div>
                </div>
                <div className="flex items-center gap-3">
                  <div className="w-20 h-1.5 rounded-full overflow-hidden" style={{ background: 'var(--border)' }}>
                    <div
                      className="h-full rounded-full"
                      style={{ width: `${seg.occupancy}%`, background: occupancyColor(seg.level) }}
                    />
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
        <h4 style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', marginBottom: '14px' }}>
          Journey timeline
        </h4>
        <ol className="relative">
          {journey.timeline.map((event, i) => (
            <li key={event.id} className="flex items-start gap-3 pb-4">
              <div className="flex flex-col items-center">
                <div
                  className="w-7 h-7 rounded-full flex items-center justify-center flex-shrink-0 z-10"
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
                {i < journey.timeline.length - 1 && (
                  <div className="w-px flex-1 mt-1" style={{ background: 'var(--border)', minHeight: '12px' }} />
                )}
              </div>
              <div className="pb-1">
                <div style={{ fontWeight: 600, color: '#1F2421', fontSize: '0.875rem' }}>{event.label}</div>
                <div style={{ color: '#4E5953', fontSize: '0.8125rem' }}>
                  {formatShort(event.timestamp)} · by {event.by}
                </div>
              </div>
            </li>
          ))}
        </ol>
      </div>

      {/* Actions */}
      {(canActivate || canComplete || canCancel) && (
        <div className="bg-white rounded-xl p-5" style={{ border: '1px solid var(--border)' }}>
          <h4 style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', marginBottom: '14px' }}>
            Journey actions
          </h4>

          {showCancelConfirm ? (
            <div>
              <div
                className="flex items-start gap-3 p-4 rounded-lg mb-4"
                style={{ background: '#FDECEA' }}
              >
                <AlertCircle size={16} color="#B42318" className="flex-shrink-0 mt-0.5" />
                <p style={{ color: '#8E1B13', fontSize: '0.875rem', lineHeight: 1.65 }}>
                  Are you sure you want to cancel this journey? This action cannot be undone. Road capacity will be released.
                </p>
              </div>
              <div className="flex gap-3">
                <button
                  onClick={() => setShowCancelConfirm(false)}
                  className="flex-1 py-2.5 rounded-lg text-sm"
                  style={{ border: '1.5px solid var(--border)', color: '#4E5953', background: 'white', fontWeight: 500 }}
                >
                  Keep journey
                </button>
                <button
                  onClick={() => handleAction('cancelled')}
                  disabled={actionLoading === 'cancelled'}
                  className="flex-1 py-2.5 rounded-lg text-white text-sm flex items-center justify-center gap-2"
                  style={{ background: '#B42318', fontWeight: 600, cursor: actionLoading ? 'not-allowed' : 'pointer' }}
                >
                  {actionLoading === 'cancelled' ? (
                    <span className="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin" />
                  ) : (
                    <XCircle size={15} />
                  )}
                  Confirm cancellation
                </button>
              </div>
            </div>
          ) : (
            <div className="flex gap-3 flex-wrap">
              {canActivate && (
                <button
                  onClick={() => handleAction('active')}
                  disabled={!!actionLoading}
                  className="flex items-center gap-2 px-5 py-2.5 rounded-lg text-white text-sm transition-colors flex-1"
                  style={{ background: '#2F6B55', fontWeight: 600, cursor: actionLoading ? 'not-allowed' : 'pointer' }}
                  onMouseEnter={(e) => !actionLoading && (e.currentTarget.style.background = '#245343')}
                  onMouseLeave={(e) => !actionLoading && (e.currentTarget.style.background = '#2F6B55')}
                >
                  {actionLoading === 'active' ? (
                    <span className="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin" />
                  ) : (
                    <Play size={15} fill="white" />
                  )}
                  Activate journey
                </button>
              )}
              {canComplete && (
                <button
                  onClick={() => handleAction('completed')}
                  disabled={!!actionLoading}
                  className="flex items-center gap-2 px-5 py-2.5 rounded-lg text-white text-sm transition-colors flex-1"
                  style={{ background: '#2F6B55', fontWeight: 600, cursor: actionLoading ? 'not-allowed' : 'pointer' }}
                  onMouseEnter={(e) => !actionLoading && (e.currentTarget.style.background = '#245343')}
                  onMouseLeave={(e) => !actionLoading && (e.currentTarget.style.background = '#2F6B55')}
                >
                  {actionLoading === 'completed' ? (
                    <span className="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin" />
                  ) : (
                    <CheckCircle2 size={15} />
                  )}
                  Complete journey
                </button>
              )}
              {canCancel && (
                <button
                  onClick={() => setShowCancelConfirm(true)}
                  className="flex items-center gap-2 px-5 py-2.5 rounded-lg text-sm transition-colors"
                  style={{ border: '1.5px solid #F5C2BE', color: '#B42318', background: 'white', fontWeight: 500 }}
                >
                  <XCircle size={15} /> Cancel journey
                </button>
              )}
            </div>
          )}

          {canActivate && (
            <p className="mt-3 text-sm" style={{ color: '#4E5953' }}>
              <strong>Activate</strong> when you're ready to depart. Make sure your vehicle is ready and you're at the origin location.
            </p>
          )}
          {canComplete && (
            <p className="mt-3 text-sm" style={{ color: '#4E5953' }}>
              <strong>Complete</strong> when you've arrived at your destination. This releases your road capacity reservation.
            </p>
          )}
        </div>
      )}
    </div>
  );
}
