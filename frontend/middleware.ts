// Vercel Edge Middleware - geo-routes /api/* to the nearest regional LB.
// Runs at Vercel's edge network before any response is served, completely
// separate from the Vite frontend bundle.
//
// Vercel injects x-vercel-ip-country on every request automatically.
// Fill in LB_US and LB_APAC once those VMs are provisioned.

const LB_EU   = 'http://34.78.55.96.nip.io'
const LB_US   = process.env.LB_US_IP   ? `http://${process.env.LB_US_IP}.nip.io`   : LB_EU  // falls back to EU until US cell is up
const LB_APAC = process.env.LB_APAC_IP ? `http://${process.env.LB_APAC_IP}.nip.io` : LB_EU  // falls back to EU until APAC cell is up

const EU_COUNTRIES = new Set([
  'GB','IE','DE','FR','NL','BE','IT','ES','PT','SE','NO','DK','FI',
  'PL','CZ','HU','RO','AT','CH','GR','HR','SK','SI','BG','LT','LV','EE',
])
const APAC_COUNTRIES = new Set([
  'AU','NZ','JP','KR','CN','HK','TW','SG','MY','TH','ID','PH','VN','IN',
])

export default async function middleware(request: Request): Promise<Response | undefined> {
  const url = new URL(request.url)

  // Only intercept API calls - let everything else through to the static bundle
  if (!url.pathname.startsWith('/api')) return undefined

  const country = request.headers.get('x-vercel-ip-country') ?? ''
  const lb = APAC_COUNTRIES.has(country) ? LB_APAC
           : EU_COUNTRIES.has(country)   ? LB_EU
           : LB_US  // US as default; falls back to EU when US/APAC not provisioned

  const backendUrl = `${lb}${url.pathname}${url.search}`
  try {
    return await fetch(new Request(backendUrl, {
      method:  request.method,
      headers: request.headers,
      body:    ['GET', 'HEAD'].includes(request.method) ? null : request.body,
    }))
  } catch {
    // Backend unreachable - return 502 instead of crashing the middleware
    return new Response(JSON.stringify({ error: 'Backend unavailable', lb }), {
      status: 502,
      headers: { 'Content-Type': 'application/json' },
    })
  }
}

// Only run this middleware on API paths
export const config = { matcher: '/api/:path*' }
