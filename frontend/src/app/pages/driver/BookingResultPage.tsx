import { ArrowRight, CheckCircle2, List, RotateCcw, XCircle } from 'lucide-react';
import { useNavigate } from 'react-router';
import { useApp } from '../../context/AppContext';

export default function BookingResultPage() {
  const navigate = useNavigate();
  const { lastBookingResult } = useApp();

  if (!lastBookingResult) {
    return (
      <div className="p-8 max-w-xl mx-auto text-center">
        <p style={{ color: '#4E5953' }}>No booking result found.</p>
        <button
          onClick={() => navigate('/driver/book')}
          className="mt-4 px-5 py-2.5 rounded-lg text-white text-sm"
          style={{ background: '#2F6B55', fontWeight: 600 }}
        >
          Book a journey
        </button>
      </div>
    );
  }

  const { success, journeyId, reason } = lastBookingResult;

  return (
    <div className="p-5 lg:p-8 max-w-xl mx-auto">
      <div className="bg-white rounded-2xl overflow-hidden" style={{ border: '1px solid var(--border)' }}>
        {/* Result header */}
        <div
          className="p-8 text-center"
          style={{
            background: success
              ? 'linear-gradient(135deg, #E8F4ED 0%, #F0F9F4 100%)'
              : 'linear-gradient(135deg, #FDECEA 0%, #FDF4F3 100%)',
          }}
        >
          <div
            className="w-16 h-16 rounded-full flex items-center justify-center mx-auto mb-4"
            style={{ background: success ? '#2F6B55' : '#B42318' }}
          >
            {success ? (
              <CheckCircle2 size={32} color="white" strokeWidth={2} />
            ) : (
              <XCircle size={32} color="white" strokeWidth={2} />
            )}
          </div>

          <h1
            style={{
              fontFamily: 'var(--font-heading)',
              fontWeight: 700,
              fontSize: '1.5rem',
              color: success ? '#1E6639' : '#8E1B13',
              marginBottom: '8px',
            }}
          >
            {success ? 'Journey approved' : 'Journey rejected'}
          </h1>

          <p style={{ color: success ? '#2F6B55' : '#B42318', fontSize: '0.9375rem' }}>
            Booking reference: <strong>{journeyId}</strong>
          </p>
        </div>

        {/* Result body */}
        <div className="p-6">
          {success ? (
            <>
              <div
                className="rounded-xl p-4 mb-5"
                style={{ background: '#F0EDE7' }}
              >
                <p style={{ color: '#1F2421', fontSize: '0.9375rem', lineHeight: 1.65, fontWeight: 500 }}>
                  Your route is clear. All road segments have available capacity at your chosen time.
                </p>
              </div>

              <h4 style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', marginBottom: '12px' }}>
                What happens next
              </h4>
              <ol className="space-y-3 mb-6">
                {[
                  'Your journey is now approved and reserved.',
                  'At departure time, open the app and activate your journey.',
                  'Drive as planned. Mark complete when you arrive.',
                ].map((step, i) => (
                  <li key={i} className="flex items-start gap-3">
                    <div
                      className="w-6 h-6 rounded-full flex items-center justify-center flex-shrink-0 text-xs mt-0.5"
                      style={{ background: '#2F6B55', color: 'white', fontWeight: 700 }}
                    >
                      {i + 1}
                    </div>
                    <p style={{ color: '#4E5953', fontSize: '0.9375rem', lineHeight: 1.55 }}>{step}</p>
                  </li>
                ))}
              </ol>

              <div className="flex gap-3">
                <button
                  onClick={() => navigate('/driver/journeys')}
                  className="flex-1 py-2.5 rounded-lg text-sm flex items-center justify-center gap-2 transition-colors"
                  style={{ border: '1.5px solid var(--border)', color: '#4E5953', background: 'white', fontWeight: 500 }}
                >
                  <List size={16} /> My journeys
                </button>
                <button
                  onClick={() => navigate(`/driver/journeys/${journeyId}`)}
                  className="flex-1 py-2.5 rounded-lg text-white text-sm flex items-center justify-center gap-2 transition-colors"
                  style={{ background: '#2F6B55', fontWeight: 600 }}
                  onMouseEnter={(e) => (e.currentTarget.style.background = '#245343')}
                  onMouseLeave={(e) => (e.currentTarget.style.background = '#2F6B55')}
                >
                  View journey <ArrowRight size={16} />
                </button>
              </div>
            </>
          ) : (
            <>
              <div
                className="rounded-xl p-4 mb-5"
                style={{ background: '#FDECEA', border: '1px solid #F5C2BE' }}
              >
                <p style={{ color: '#8E1B13', fontSize: '0.9375rem', lineHeight: 1.65 }}>
                  {reason || 'Your journey could not be booked due to road capacity limits at the chosen time.'}
                </p>
              </div>

              <h4 style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, color: '#1F2421', marginBottom: '12px' }}>
                What you can do
              </h4>
              <ul className="space-y-2 mb-6">
                {[
                  'Choose a later departure time - after 09:30 usually has more availability.',
                  'Try a different route or check the traffic map for congestion.',
                  'Consider an off-peak time such as midday or early evening.',
                ].map((tip, i) => (
                  <li key={i} className="flex items-start gap-2">
                    <span style={{ color: '#2F6B55', fontWeight: 700, fontSize: '1rem', lineHeight: 1.4 }}>·</span>
                    <p style={{ color: '#4E5953', fontSize: '0.9375rem', lineHeight: 1.55 }}>{tip}</p>
                  </li>
                ))}
              </ul>

              <div className="flex gap-3">
                <button
                  onClick={() => navigate('/driver/journeys')}
                  className="flex-1 py-2.5 rounded-lg text-sm flex items-center justify-center gap-2 transition-colors"
                  style={{ border: '1.5px solid var(--border)', color: '#4E5953', background: 'white', fontWeight: 500 }}
                >
                  <List size={16} /> My journeys
                </button>
                <button
                  onClick={() => navigate('/driver/book')}
                  className="flex-1 py-2.5 rounded-lg text-white text-sm flex items-center justify-center gap-2 transition-colors"
                  style={{ background: '#2F6B55', fontWeight: 600 }}
                  onMouseEnter={(e) => (e.currentTarget.style.background = '#245343')}
                  onMouseLeave={(e) => (e.currentTarget.style.background = '#2F6B55')}
                >
                  <RotateCcw size={16} /> Try again
                </button>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
}
