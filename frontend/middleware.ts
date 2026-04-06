// Vercel Edge Middleware — geo-routes /api/* to the nearest regional LB.
// Runs at Vercel's edge network before any response is served, completely
// separate from the Vite frontend bundle.
//
// Vercel injects x-vercel-ip-country on every request automatically.
// Fill in LB_US and LB_APAC once those VMs are provisioned.

const LB_EU   = 'http://34.78.55.96'
const LB_US   = 'http://PLACEHOLDER_US_LB_IP'    // TODO: replace when US cell is up
const LB_APAC = 'http://PLACEHOLDER_APAC_LB_IP'  // TODO: replace when APAC cell is up

const EU_COUNTRIES = new Set([
  'GB','IE','DE','FR','NL','BE','IT','ES','PT','SE','NO','DK','FI',
  'PL','CZ','HU','RO','AT','CH','GR','HR','SK','SI','BG','LT','LV','EE',
])
const APAC_COUNTRIES = new Set([
  'AU','NZ','JP','KR','CN','HK','TW','SG','MY','TH','ID','PH','VN','IN',
])

export default async function middleware(request: Request): Promise<Response | undefined> {
  const url = new URL(request.url)

  // Only intercept API calls — let everything else through to the static bundle
  if (!url.pathname.startsWith('/api')) return undefined

  const country = request.headers.get('x-vercel-ip-country') ?? ''
  const lb = APAC_COUNTRIES.has(country) ? LB_APAC
           : EU_COUNTRIES.has(country)   ? LB_EU
           : LB_US  // US as default for unlisted countries

  // Forward the full request (method + headers + body) to the regional backend
  const backendUrl = `${lb}${url.pathname}${url.search}`
  return fetch(new Request(backendUrl, {
    method:  request.method,
    headers: request.headers,
    body:    ['GET', 'HEAD'].includes(request.method) ? null : request.body,
  }))
}

// Only run this middleware on API paths
export const config = { matcher: '/api/:path*' }
