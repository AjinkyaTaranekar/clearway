import { useState } from 'react';
import { useNavigate } from 'react-router';
import { useApp, UserRole } from '../context/AppContext';
import { Eye, EyeOff, AlertCircle, CheckCircle2 } from 'lucide-react';

const features = [
  { icon: '🗺️', title: 'Pre-book your route', desc: 'Check road capacity before you drive' },
  { icon: '⚡', title: 'Instant decisions', desc: 'Get approved or rejected in seconds' },
  { icon: '📍', title: 'Live journey tracking', desc: 'Follow your journey status in real time' },
];

export default function LoginPage() {
  const navigate = useNavigate();
  const { login, isAuthenticated, user } = useApp();

  const [role, setRole] = useState<UserRole>('driver');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [errors, setErrors] = useState<{ email?: string; password?: string; form?: string }>({});
  const [loading, setLoading] = useState(false);

  // Redirect if already authenticated
  if (isAuthenticated && user) {
    if (user.role === 'admin') navigate('/admin');
    else navigate('/driver');
  }

  const validate = () => {
    const e: typeof errors = {};
    if (!email) e.email = 'Email is required.';
    else if (!/\S+@\S+\.\S+/.test(email)) e.email = 'Enter a valid email address.';
    if (!password) e.password = 'Password is required.';
    else if (password.length < 6) e.password = 'Password must be at least 6 characters.';
    return e;
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const errs = validate();
    if (Object.keys(errs).length > 0) {
      setErrors(errs);
      return;
    }
    setErrors({});
    setLoading(true);
    await new Promise((r) => setTimeout(r, 900));
    login(email, password, role);
    setLoading(false);
    navigate(role === 'admin' ? '/admin' : '/driver');
  };

  return (
    <div className="min-h-screen flex" style={{ background: '#F8F6F2' }}>
      {/* Left brand panel */}
      <div
        className="hidden lg:flex flex-col justify-between w-[480px] flex-shrink-0 p-10"
        style={{ background: '#1F2421', color: 'white' }}
      >
        <div>
          <div className="flex items-center gap-3 mb-16">
            <div
              className="w-10 h-10 rounded-xl flex items-center justify-center"
              style={{ background: '#2F6B55' }}
            >
              <svg width="20" height="20" viewBox="0 0 16 16" fill="none">
                <path d="M8 1.5L14 8L8 14.5L2 8L8 1.5Z" fill="white" opacity="0.25" />
                <path d="M8 4L11.5 8L8 12L4.5 8L8 4Z" fill="white" opacity="0.55" />
                <circle cx="8" cy="8" r="2.5" fill="white" />
              </svg>
            </div>
            <div>
              <div style={{ fontFamily: 'var(--font-heading)', fontWeight: 800, fontSize: '1.25rem', letterSpacing: '-0.01em' }}>
                Clearway
              </div>
              <div style={{ fontSize: '0.7rem', color: 'rgba(255,255,255,0.45)', letterSpacing: '0.05em', textTransform: 'uppercase', fontWeight: 500 }}>
                Road Journey Platform
              </div>
            </div>
          </div>

          <div className="mb-10">
            <h2 style={{ fontFamily: 'var(--font-heading)', fontWeight: 700, fontSize: '2rem', lineHeight: 1.2, marginBottom: '1rem', color: 'white' }}>
              Drive ahead,<br />always in the clear.
            </h2>
            <p style={{ color: 'rgba(255,255,255,0.55)', fontSize: '1rem', lineHeight: 1.65 }}>
              Pre-book your road journey and know before you go. Clearway checks every segment of your route so you drive with confidence.
            </p>
          </div>

          <div className="space-y-5">
            {features.map((f) => (
              <div key={f.title} className="flex items-start gap-4">
                <div
                  className="w-10 h-10 rounded-lg flex items-center justify-center flex-shrink-0 text-lg"
                  style={{ background: 'rgba(255,255,255,0.06)' }}
                >
                  {f.icon}
                </div>
                <div>
                  <div style={{ fontFamily: 'var(--font-heading)', fontWeight: 600, fontSize: '0.9375rem', color: 'white', marginBottom: '2px' }}>
                    {f.title}
                  </div>
                  <div style={{ color: 'rgba(255,255,255,0.45)', fontSize: '0.875rem' }}>
                    {f.desc}
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>

        <div style={{ color: 'rgba(255,255,255,0.2)', fontSize: '0.8rem' }}>
          © 2026 Clearway. Distributed Traffic Service.
        </div>
      </div>

      {/* Right auth panel */}
      <div className="flex-1 flex items-center justify-center p-6 lg:p-12">
        <div className="w-full max-w-md">
          {/* Mobile logo */}
          <div className="flex items-center gap-2.5 mb-8 lg:hidden">
            <div
              className="w-9 h-9 rounded-xl flex items-center justify-center"
              style={{ background: '#2F6B55' }}
            >
              <svg width="18" height="18" viewBox="0 0 16 16" fill="none">
                <circle cx="8" cy="8" r="2.5" fill="white" />
                <path d="M8 4L11.5 8L8 12L4.5 8L8 4Z" fill="white" opacity="0.5" />
              </svg>
            </div>
            <span style={{ fontFamily: 'var(--font-heading)', fontWeight: 800, fontSize: '1.25rem', color: '#1F2421' }}>
              Clearway
            </span>
          </div>

          <div className="bg-white rounded-2xl p-8 shadow-sm" style={{ border: '1px solid var(--border)' }}>
            <div className="mb-7">
              <h1 style={{ fontFamily: 'var(--font-heading)', fontWeight: 700, fontSize: '1.5rem', color: '#1F2421', marginBottom: '4px' }}>
                Welcome back
              </h1>
              <p style={{ color: '#4E5953', fontSize: '0.9375rem' }}>
                Sign in to your account to continue.
              </p>
            </div>

            {/* Role selector */}
            <div
              className="flex rounded-lg p-1 mb-6"
              style={{ background: '#F0EDE7' }}
            >
              {(['driver', 'admin'] as UserRole[]).map((r) => (
                <button
                  key={r}
                  onClick={() => setRole(r)}
                  className="flex-1 py-2 rounded-md text-sm transition-all duration-150"
                  style={{
                    background: role === r ? 'white' : 'transparent',
                    color: role === r ? '#1F2421' : '#4E5953',
                    fontWeight: role === r ? 600 : 400,
                    boxShadow: role === r ? '0 1px 3px rgba(0,0,0,0.08)' : 'none',
                    fontFamily: 'var(--font-body)',
                  }}
                >
                  {r === 'driver' ? '🚗 Driver' : '⚙️ Admin'}
                </button>
              ))}
            </div>

            <form onSubmit={handleSubmit} noValidate>
              {errors.form && (
                <div
                  className="flex items-center gap-2 p-3 rounded-lg mb-4"
                  style={{ background: 'var(--status-rejected-bg)', color: 'var(--status-rejected-text)' }}
                  role="alert"
                >
                  <AlertCircle size={16} />
                  <span className="text-sm">{errors.form}</span>
                </div>
              )}

              <div className="space-y-4">
                <div>
                  <label
                    htmlFor="email"
                    className="block mb-1.5"
                    style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}
                  >
                    Email address
                  </label>
                  <input
                    id="email"
                    type="email"
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    placeholder={role === 'driver' ? 'driver@example.com' : 'admin@example.com'}
                    aria-invalid={!!errors.email}
                    aria-describedby={errors.email ? 'email-error' : undefined}
                    className="w-full px-3.5 py-2.5 rounded-lg outline-none transition-all"
                    style={{
                      border: errors.email ? '1.5px solid #B42318' : '1.5px solid var(--border)',
                      background: 'white',
                      color: '#1F2421',
                    }}
                    onFocus={(e) => (e.target.style.borderColor = '#2F6B55')}
                    onBlur={(e) => (e.target.style.borderColor = errors.email ? '#B42318' : 'var(--border)')}
                  />
                  {errors.email && (
                    <p id="email-error" className="mt-1.5 text-sm" style={{ color: '#B42318' }} role="alert">
                      {errors.email}
                    </p>
                  )}
                </div>

                <div>
                  <div className="flex items-center justify-between mb-1.5">
                    <label
                      htmlFor="password"
                      style={{ color: '#1F2421', fontSize: '0.875rem', fontWeight: 500 }}
                    >
                      Password
                    </label>
                    <button
                      type="button"
                      className="text-sm"
                      style={{ color: '#2F6B55', fontWeight: 500 }}
                    >
                      Forgot password?
                    </button>
                  </div>
                  <div className="relative">
                    <input
                      id="password"
                      type={showPassword ? 'text' : 'password'}
                      value={password}
                      onChange={(e) => setPassword(e.target.value)}
                      placeholder="••••••••"
                      aria-invalid={!!errors.password}
                      aria-describedby={errors.password ? 'password-error' : undefined}
                      className="w-full px-3.5 py-2.5 pr-10 rounded-lg outline-none transition-all"
                      style={{
                        border: errors.password ? '1.5px solid #B42318' : '1.5px solid var(--border)',
                        background: 'white',
                        color: '#1F2421',
                      }}
                      onFocus={(e) => (e.target.style.borderColor = '#2F6B55')}
                      onBlur={(e) => (e.target.style.borderColor = errors.password ? '#B42318' : 'var(--border)')}
                    />
                    <button
                      type="button"
                      onClick={() => setShowPassword(!showPassword)}
                      className="absolute right-3 top-1/2 -translate-y-1/2"
                      aria-label={showPassword ? 'Hide password' : 'Show password'}
                    >
                      {showPassword ? (
                        <EyeOff size={16} color="#4E5953" />
                      ) : (
                        <Eye size={16} color="#4E5953" />
                      )}
                    </button>
                  </div>
                  {errors.password && (
                    <p id="password-error" className="mt-1.5 text-sm" style={{ color: '#B42318' }} role="alert">
                      {errors.password}
                    </p>
                  )}
                </div>
              </div>

              <button
                type="submit"
                disabled={loading}
                className="w-full mt-6 py-3 rounded-lg transition-all duration-150 flex items-center justify-center gap-2"
                style={{
                  background: loading ? '#4E5953' : '#2F6B55',
                  color: 'white',
                  fontWeight: 600,
                  fontSize: '0.9375rem',
                  cursor: loading ? 'not-allowed' : 'pointer',
                }}
                onMouseEnter={(e) => !loading && (e.currentTarget.style.background = '#245343')}
                onMouseLeave={(e) => !loading && (e.currentTarget.style.background = '#2F6B55')}
              >
                {loading ? (
                  <>
                    <span className="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin" />
                    Signing in…
                  </>
                ) : (
                  'Sign in'
                )}
              </button>
            </form>

            <div className="mt-5 pt-5" style={{ borderTop: '1px solid var(--border)' }}>
              <p className="text-center text-sm" style={{ color: '#4E5953' }}>
                Don't have an account?{' '}
                <button className="font-medium" style={{ color: '#2F6B55' }}>
                  Create one
                </button>
              </p>
            </div>

            {/* Demo hint */}
            <div
              className="mt-4 p-3 rounded-lg flex items-start gap-2"
              style={{ background: '#F0EDE7' }}
            >
              <CheckCircle2 size={15} color="#2F6B55" className="mt-0.5 flex-shrink-0" />
              <p style={{ color: '#4E5953', fontSize: '0.8125rem', lineHeight: 1.5 }}>
                <strong style={{ color: '#1F2421' }}>Demo mode.</strong> Enter any valid email and a password of 6+ characters to sign in.
              </p>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
