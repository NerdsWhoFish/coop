import { useCallback, useEffect, useEffectEvent, useMemo, useRef, useState } from 'react'
import { Link, NavLink, Navigate, Route, Routes, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { QRCodeSVG } from 'qrcode.react'
import {
  ArrowLeft, Bell, Check, ChevronRight, Compass, Heart, Home as HomeIcon, LoaderCircle,
  LockKeyhole, Menu, Play, Search, Share2, Sparkles, ThumbsDown, ThumbsUp,
  TvMinimalPlay, Users, X,
} from 'lucide-react'
import { api, APIError } from './api'
import type { Channel, ChannelPage, ChildProfile, ChildRequest, Discovery, Video, WatchPage as WatchData, WebLink } from './types'
import { usePlaybackLease } from './usePlaybackLease'
import { YouTubePlayer } from './YouTubePlayer'
import { DeviceSessionAction } from './DeviceSessionAction'

function App() {
  const [profile, setProfile] = useState<ChildProfile | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const restore = useCallback(async () => {
    setLoading(true)
    try { setProfile(await api.me()) }
    catch (cause) {
      if (!(cause instanceof APIError && cause.status === 401)) setError(message(cause))
      setProfile(null)
    } finally { setLoading(false) }
  }, [])

  useEffect(() => { void Promise.resolve().then(restore) }, [restore])
  if (loading) return <Splash />
  if (!profile) return <LinkDevice onLinked={setProfile} />
  return <Shell profile={profile} onProfile={setProfile} onLogout={() => setProfile(null)} error={error} clearError={() => setError(null)} setError={setError} />
}

function Splash() {
  return <main className="splash"><div className="coop-mark"><Play fill="currentColor" /></div><h1>Cooper Watch</h1><LoaderCircle className="spin" aria-label="Loading" /></main>
}

function LinkDevice({ onLinked }: { onLinked: (profile: ChildProfile) => void }) {
  const [link, setLink] = useState<WebLink | null>(null)
  const [code, setCode] = useState('')
  const [usingCode, setUsingCode] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [working, setWorking] = useState(false)
  const deviceName = useMemo(() => `${browserName()} on ${platformName()}`, [])

  const begin = useCallback(async () => {
    setWorking(true); setError(null)
    try { setLink(await api.createLink(deviceName)) }
    catch (cause) { setError(message(cause)) }
    finally { setWorking(false) }
  }, [deviceName])

  useEffect(() => { void Promise.resolve().then(begin) }, [begin])
  useEffect(() => {
    if (!link) return
    const expires = Date.parse(link.expiresAt)
    const timer = window.setInterval(async () => {
      if (Date.now() >= expires) { window.clearInterval(timer); setLink(null); setError('That code expired. Make a fresh one and try again.'); return }
      try {
        const status = await api.linkStatus(link)
        if (status.approved) { window.clearInterval(timer); onLinked(await api.redeemLink(link)) }
      } catch (cause) {
        if (cause instanceof APIError && cause.status === 404) return
        setError(message(cause))
      }
    }, 1600)
    return () => window.clearInterval(timer)
  }, [link, onLinked])

  async function pair(event: React.FormEvent) {
    event.preventDefault(); setWorking(true); setError(null)
    try { onLinked(await api.pair(code, deviceName)) }
    catch (cause) { setError(message(cause)) }
    finally { setWorking(false) }
  }

  return <main className="link-page">
    <section className="link-story">
      <div className="brand-lockup"><div className="coop-mark"><Play fill="currentColor" /></div><span>COOPER WATCH</span></div>
      <div><p className="eyebrow">YOUR COOP, BIGGER</p><h1>Bring your shelf<br />to this screen.</h1><p className="lede">Everything you follow. Every parent rule. One quick scan.</p></div>
      <div className="signal" aria-hidden="true"><i /><i /><i /><span><TvMinimalPlay /></span></div>
    </section>
    <section className="link-card" aria-live="polite">
      {!usingCode ? <>
        <div className="step-pill">1 MINUTE SETUP</div>
        <h2>Scan with a Coop app</h2>
        <p>Open Cooper Watch and choose <strong>Link a computer</strong>, or scan from your profile in Cooper The Cop.</p>
        <div className="qr-frame">
          {link ? <QRCodeSVG value={link.qrPayload} size={220} level="M" bgColor="#ffffff" fgColor="#191a21" title="Computer link code" /> : <LoaderCircle className="spin" />}
          <span className="qr-corner a" /><span className="qr-corner b" /><span className="qr-corner c" /><span className="qr-corner d" />
        </div>
        <div className="waiting"><span className="pulse" /> Waiting for a scan</div>
        {error && <InlineError text={error} />}
        {!link && <button className="primary" onClick={() => void begin()} disabled={working}>Make a new code</button>}
        <button className="text-button" onClick={() => setUsingCode(true)}>Use a one-time pairing code instead</button>
      </> : <>
        <button className="back-link" onClick={() => setUsingCode(false)}><ArrowLeft /> Back to QR scan</button>
        <div className="step-pill">PAIRING CODE</div><h2>Type the code from a parent</h2>
        <p>Ask a parent to create a one-time code for your profile.</p>
        <form onSubmit={pair} className="pair-form">
          <label htmlFor="pair-code">Pairing code</label>
          <input id="pair-code" value={code} onChange={event => setCode(event.target.value.toUpperCase())} placeholder="ABCD-EFGH" autoComplete="one-time-code" maxLength={12} autoFocus />
          <button className="primary" disabled={working || code.length < 8}>{working ? <LoaderCircle className="spin" /> : 'Open my Coop'}</button>
        </form>
        {error && <InlineError text={error} />}
      </>}
      <small>This browser will appear as “{deviceName}” in the parent app.</small>
    </section>
  </main>
}

type ShellProps = { profile: ChildProfile; onProfile: (profile: ChildProfile) => void; onLogout: () => void; error: string | null; clearError: () => void; setError: (text: string) => void }
function Shell({ profile, onProfile, onLogout, error, clearError, setError }: ShellProps) {
  const location = useLocation()
  const navigate = useNavigate()
  const [menuOpen, setMenuOpen] = useState(false)
  const refreshProfile = useCallback(() => api.me().then(onProfile), [onProfile])
  useEffect(() => { window.scrollTo(0, 0) }, [location.pathname, location.search])
  useEffect(() => {
    const refreshOnFocus = () => { void refreshProfile().catch(() => {}) }
    window.addEventListener('focus', refreshOnFocus)
    return () => window.removeEventListener('focus', refreshOnFocus)
  }, [refreshProfile])
  async function logout() {
    try {
      await api.logout()
      onLogout()
      navigate('/')
    } catch (cause) {
      setMenuOpen(false)
      setError(message(cause))
    }
  }
  const nav = [
    ['/', 'Home', HomeIcon],
    ...(profile.shortsEnabled ? [['/shorts', 'Shorts', Sparkles] as const] : []),
    ['/subscriptions', 'Subscriptions', Users],
    ['/search', 'Search', Search],
  ] as const
  const viewing = location.pathname.startsWith('/watch/') || location.pathname.startsWith('/channel/')
  const immersive = location.pathname === '/shorts'
  return <div className="app-shell">
    <header className="topbar">
      <Link to="/" className="brand-lockup"><div className="coop-mark small"><Play fill="currentColor" /></div><span>COOPER WATCH</span></Link>
      <nav className="desktop-nav" aria-label="Main navigation">{nav.map(([to, label, Icon]) => <NavLink key={to} to={to} end={to === '/'}><Icon />{label}</NavLink>)}</nav>
      <button className="profile-button" onClick={() => setMenuOpen(!menuOpen)} aria-expanded={menuOpen}><span>{profile.name.slice(0, 1)}</span><Menu /></button>
      {menuOpen && <div className="profile-menu"><strong>{profile.name}</strong><small>This computer</small><DeviceSessionAction allowSelfUnpair={profile.allowSelfUnpair} onLogout={() => void logout()} /></div>}
    </header>
    <main className={`content${viewing ? ' viewing' : ''}${immersive ? ' immersive' : ''}`}>
      <Routes>
        <Route path="/" element={<HomePage profile={profile} onProfile={onProfile} setError={setError} />} />
        <Route path="/shorts" element={profile.shortsEnabled ? <ShortsPage setError={setError} /> : <Navigate to="/" />} />
        <Route path="/subscriptions" element={<SubscriptionsPage onProfile={onProfile} setError={setError} />} />
        <Route path="/search" element={<SearchPage profile={profile} setError={setError} />} />
        <Route path="/channel/:id" element={<ChannelView profile={profile} setError={setError} />} />
        <Route path="/watch/:id" element={<WatchPage setError={setError} />} />
        <Route path="/requests" element={<RequestsPage setError={setError} />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </main>
    <nav className="mobile-nav" aria-label="Main navigation">{nav.map(([to, label, Icon]) => <NavLink key={to} to={to} end={to === '/'}><Icon /><span>{label}</span></NavLink>)}</nav>
    {error && <div className="toast" role="alert"><span>{error}</span><button onClick={clearError} aria-label="Dismiss"><X /></button></div>}
  </div>
}

function HomePage({ profile, onProfile, setError }: { profile: ChildProfile; onProfile: (profile: ChildProfile) => void; setError: (s: string) => void }) {
  const [feed, setFeed] = useState<Video[]>([]), [subscriptions, setSubscriptions] = useState<Channel[]>([]), [discoveries, setDiscoveries] = useState<Discovery[]>([])
  const [loading, setLoading] = useState(true)
  const [phoneSection, setPhoneSection] = useState<'recommendations' | 'subscriptions' | 'discover'>('recommendations')
  const load = useCallback(async () => {
    setLoading(true)
    try { const [f, s, d, nextProfile] = await Promise.all([api.feed(), api.subscriptions(), api.discovery().catch(() => []), api.me()]); setFeed(f); setSubscriptions(s); setDiscoveries(d); onProfile(nextProfile) }
    catch (cause) { setError(message(cause)) } finally { setLoading(false) }
  }, [onProfile, setError])
  useEffect(() => { void Promise.resolve().then(load) }, [load])
  const followed = new Set(subscriptions.map(channel => channel.id))
  const subscriptionVideos = feed.filter(video => followed.has(video.channelId))
  return <>
    <PageHeading eyebrow="YOUR COOP" title={`Hey, ${profile.name}!`} action={<Link className="icon-action" to="/requests"><Bell /><span>Requests</span></Link>} />
    <div className="home-section-picker" role="tablist" aria-label="Home sections">{(['recommendations', 'subscriptions', 'discover'] as const).map(section => <button key={section} role="tab" aria-selected={phoneSection === section} onClick={() => setPhoneSection(section)}>{section === 'discover' ? 'Discover' : section[0].toUpperCase() + section.slice(1)}</button>)}</div>
    {loading ? <ShelfSkeletons /> : <div className="shelves" data-phone-section={phoneSection}>
      <VideoShelf title="Recommendations" icon={<Sparkles />} videos={feed} empty="Recommendations will show up as you watch." />
      <VideoShelf title="Subscriptions" icon={<Heart />} videos={subscriptionVideos} empty="Subscribe to a channel to keep its videos here." />
      <DiscoveryShelf discoveries={discoveries} />
    </div>}
  </>
}

function VideoShelf({ title, icon, videos, empty }: { title: string; icon: React.ReactNode; videos: Video[]; empty: string }) {
  return <section className="shelf"><div className="section-heading"><span>{icon}</span><h2>{title}</h2><i /></div>
    {videos.length ? <div className="video-rail">{videos.map(video => <VideoCard key={video.id} video={video} />)}</div> : <EmptyLine text={empty} />}
  </section>
}

function DiscoveryShelf({ discoveries }: { discoveries: Discovery[] }) {
  return <section className="shelf discover"><div className="section-heading"><span><Compass /></span><h2>Discover</h2><b>NEW CHANNELS</b><i /></div>
    <p className="section-explainer">These match what you enjoy, but a grown-up still has to unlock the channel.</p>
    {discoveries.length ? <div className="video-rail">{discoveries.map(item => <VideoCard key={item.video.id} video={item.video} reason={item.reason} locked pendingRequest={item.pendingRequest} />)}</div> : <EmptyLine text="Parent-approved new channel ideas will show up here." />}
  </section>
}

type VideoCardProps = { video: Video; reason?: string; locked?: boolean; pendingRequest?: boolean; onAsk?: () => void }
function VideoCard({ video, reason, locked = false, pendingRequest = false, onAsk }: VideoCardProps) {
  const isLocked = locked || video.locked
  const target = isLocked ? `/channel/${video.channelId}?video=${encodeURIComponent(video.id)}` : `/watch/${video.id}`
  const state = locked || video.locked ? undefined : { video }
  const artwork = <><div className="image-fallback"><Play /></div>{video.thumbnailUrl && <img src={video.thumbnailUrl} alt="" loading="lazy" onError={event => { event.currentTarget.style.display = 'none' }} />}{isLocked ? <span className={`ask-badge${pendingRequest ? ' pending' : ''}`}>{pendingRequest ? <><Check /> ASKED</> : <><LockKeyhole /> ASK</>}</span> : <span className="duration">{duration(video.durationSeconds)}</span>}</>
  return <article className="video-card">
    {isLocked && onAsk ? <button className="thumbnail" onClick={onAsk} disabled={pendingRequest} aria-label={pendingRequest ? `Already asked for ${video.title}` : `Ask to watch ${video.title}`}>{artwork}</button> : <Link to={target} state={state} className="thumbnail">{artwork}</Link>}
    <div className="video-info">{isLocked && onAsk ? <button className="video-title text-link" onClick={onAsk} disabled={pendingRequest}>{video.title}</button> : <Link className="video-title" to={target} state={state}>{video.title}</Link>}
      <Link className="channel-link" to={`/channel/${video.channelId}?video=${encodeURIComponent(video.id)}`}>{video.channelTitle ?? 'Channel'}</Link>{reason && <small>{reason}</small>}{isLocked && <small className={pendingRequest ? 'request-waiting' : 'request-preview'}>{pendingRequest ? 'Waiting for a grown-up' : 'Preview channel'}</small>}
    </div>
  </article>
}

function SubscriptionsPage({ onProfile, setError }: { onProfile: (profile: ChildProfile) => void; setError: (s: string) => void }) {
  const [channels, setChannels] = useState<Channel[]>([]), [loading, setLoading] = useState(true)
  const load = useCallback(async () => {
    setLoading(true)
    try { const [nextChannels, nextProfile] = await Promise.all([api.subscriptions(), api.me()]); setChannels(nextChannels); onProfile(nextProfile) }
    catch (cause) { setError(message(cause)) }
    finally { setLoading(false) }
  }, [onProfile, setError])
  useEffect(() => { void Promise.resolve().then(load) }, [load])
  return <><PageHeading eyebrow="YOUR FAVORITES" title="Subscriptions" />{loading ? <ShelfSkeletons /> : channels.length ? <div className="channel-grid">{channels.map(channel => <ChannelCard key={channel.id} channel={channel} />)}</div> : <EmptyState icon={<Users />} title="No subscriptions yet" text="Open an approved channel and tap Subscribe." />}</>
}

function ChannelCard({ channel }: { channel: Channel }) {
  const requestable = channel.state === 'requestable'
  return <Link className="channel-card" to={`/channel/${channel.id}`}><div className="channel-avatar"><span>{channel.title.slice(0, 1)}</span>{channel.thumbnailUrl && <img src={channel.thumbnailUrl} alt="" onError={event => { event.currentTarget.style.display = 'none' }} />}</div><div><strong>{channel.title}</strong><span>{requestable ? channel.pendingRequest ? <><Check /> Asked</> : <><LockKeyhole /> Ask a parent</> : <>Open channel <ChevronRight /></>}</span></div></Link>
}

function SearchPage({ profile, setError }: { profile: ChildProfile; setError: (s: string) => void }) {
  const [params, setParams] = useSearchParams(), query = params.get('q') ?? '', channelId = params.get('channel') ?? undefined, channelTitle = params.get('channelName') ?? undefined
  const [draft, setDraft] = useState(query), [channels, setChannels] = useState<Channel[]>([]), [videos, setVideos] = useState<Video[]>([]), [loading, setLoading] = useState(false), [askedChannels, setAskedChannels] = useState<Set<string>>(new Set())
  const search = useCallback(() => {
    if (!query) { setChannels([]); setVideos([]); return Promise.resolve() }
    setLoading(true)
    return api.search(query, channelId).then(result => { setChannels(result.channels); setVideos(result.videos) }).catch(c => setError(message(c))).finally(() => setLoading(false))
  }, [query, channelId, setError])
  useEffect(() => { void Promise.resolve().then(() => { setDraft(query); return search() }) }, [query, search])
  function submit(event: React.FormEvent) { event.preventDefault(); setParams({ q: draft, ...(channelId ? { channel: channelId, channelName: channelTitle ?? '' } : {}) }) }
  async function ask(video: Video) {
    try {
      await api.ask(video.channelId, video.id)
      setAskedChannels(current => new Set(current).add(video.channelId))
    } catch (cause) { setError(message(cause)) }
  }
  if (channelId && !profile.videoSearchTiles) return <Navigate to={`/channel/${channelId}`} replace />
  return <><PageHeading eyebrow={channelTitle ? `SEARCHING ${channelTitle.toUpperCase()}` : 'FIND SOMETHING'} title={channelTitle ? `Videos from ${channelTitle}` : 'Search'} />
    <form className="search-box" onSubmit={submit}><Search /><input value={draft} onChange={e => setDraft(e.target.value)} placeholder={channelTitle ? `Search ${channelTitle}` : 'Channels and videos'} aria-label="Search channels and videos" /><button>Search</button></form>
    {channelId && <Link className="scope-chip" to={`/channel/${channelId}`}><ArrowLeft /> Back to {channelTitle}</Link>}
    {loading ? <ShelfSkeletons /> : query ? <div className="search-results">{channels.length > 0 && <section><h2>Channels</h2><div className="channel-grid">{channels.map(c => <ChannelCard key={c.id} channel={c} />)}</div></section>}<section><h2>Videos</h2>{videos.length ? <div className="video-grid">{videos.map(v => <VideoCard key={v.id} video={v} locked={v.locked} pendingRequest={askedChannels.has(v.channelId)} onAsk={v.locked ? () => void ask(v) : undefined} />)}</div> : <EmptyLine text="No videos found. Try another search." />}</section></div> : <EmptyState icon={<Search />} title="What sounds good?" text="Search approved videos and channels. New channels stay locked until a parent says yes." />}
  </>
}

function ChannelView({ profile, setError }: { profile: ChildProfile; setError: (s: string) => void }) {
  const { id = '' } = useParams(), [params] = useSearchParams(), promptedByVideoId = params.get('video') ?? undefined
  const [page, setPage] = useState<ChannelPage | null>(null), [working, setWorking] = useState(false)
  const load = useCallback(() => api.channel(id).then(setPage).catch(c => setError(message(c))), [id, setError])
  useEffect(() => { void Promise.resolve().then(load) }, [load])
  if (!page) return <ShelfSkeletons />
  async function toggle() {
    if (!page) return
    const current = page
    setWorking(true)
    try { await api.subscribe(id, !current.subscribed); setPage({ ...current, subscribed: !current.subscribed }) }
    catch (c) { setError(message(c)) } finally { setWorking(false) }
  }
  async function ask() {
    if (!page) return
    const current = page
    setWorking(true)
    try { await api.ask(id, promptedByVideoId); setPage({ ...current, pendingRequest: true }) }
    catch (c) { setError(message(c)) } finally { setWorking(false) }
  }
  return <>
    <Link to="/" className="back-link"><ArrowLeft /> Home</Link>
    <section className="channel-hero" style={page.channel.bannerUrl ? { backgroundImage: `linear-gradient(90deg, #282a36 15%, transparent), url(${page.channel.bannerUrl})` } : undefined}>
      <div className="channel-avatar large">{page.channel.thumbnailUrl ? <img src={page.channel.thumbnailUrl} alt="" /> : page.channel.title.slice(0, 1)}</div>
      <div><p className="eyebrow">CHANNEL</p><h1>{page.channel.title}</h1><p>{page.channel.description || 'Videos from this channel.'}</p><div className="button-row">
        {page.state === 'allowed' ? <button className={page.subscribed ? 'secondary selected' : 'primary'} onClick={() => void toggle()} disabled={working}>{page.subscribed ? <><Check /> Subscribed</> : 'Subscribe'}</button> : <button className="ask-button" onClick={() => void ask()} disabled={working || page.pendingRequest}>{page.pendingRequest ? <><Check /> Asked</> : <><LockKeyhole /> Ask a parent</>}</button>}
        {profile.videoSearchTiles && <Link className="secondary" to={`/search?channel=${id}&channelName=${encodeURIComponent(page.channel.title)}`}><Search /> Search channel</Link>}
      </div></div>
    </section>
    {page.state === 'allowed' && <section className="shelf"><div className="section-heading"><span><Play /></span><h2>Videos</h2><i /></div>{page.videos.length ? <div className="video-grid">{page.videos.map(v => <VideoCard key={v.id} video={v} />)}</div> : <EmptyLine text="No videos have arrived yet." />}</section>}
  </>
}

function WatchPage({ setError }: { setError: (s: string) => void }) {
  const { id = '' } = useParams()
  return <WatchPageContent key={id} id={id} setError={setError} />
}

function WatchPageContent({ id, setError }: { id: string; setError: (s: string) => void }) {
  const location = useLocation()
  const navigate = useNavigate()
  const pendingVideo = (location.state as { video?: Video } | null)?.video
  const [page, setPage] = useState<WatchData | null>(null), [related, setRelated] = useState<Video[]>([]), [discoveries, setDiscoveries] = useState<Discovery[]>([]), [subscribed, setSubscribed] = useState(false)
  const blocked = useCallback((notice: string) => {
    setError(notice)
    navigate('/', { replace: true })
  }, [navigate, setError])
  usePlaybackLease({ videoId: id, active: page !== null, onBlocked: blocked, onError: setError })
  const load = useCallback(async () => {
    try {
      const [watch, feed, discovered, subs] = await Promise.all([api.watch(id), api.feed(), api.discovery().catch(() => []), api.subscriptions()])
      setPage(watch)
      setRelated(feed.filter(v => v.id !== id).slice(0, 12))
      setDiscoveries(discovered.filter(item => item.video.id !== id).slice(0, 4))
      setSubscribed(subs.some(c => c.id === watch.video.channelId))
    } catch (cause) { setError(message(cause)) }
  }, [id, setError])
  useEffect(() => { void Promise.resolve().then(load) }, [load])
  if (!page) return <div className="player-loading" style={pendingVideo?.thumbnailUrl ? { backgroundImage: `url(${pendingVideo.thumbnailUrl})` } : undefined}><div><LoaderCircle className="spin" /><span>Getting the video ready…</span></div></div>
  async function react(kind: 'like' | 'dislike') {
    if (!page) return
    const current = page, next = current.reaction === kind ? undefined : kind
    try { await api.react(id, next); setPage({ ...current, reaction: next }) } catch (c) { setError(message(c)) }
  }
  async function subscribe() {
    if (!page) return
    try { await api.subscribe(page.video.channelId, !subscribed); setSubscribed(!subscribed) } catch (c) { setError(message(c)) }
  }
  return <div className="watch-layout">
    <section className="watch-main">
      <div className="player-shell"><YouTubePlayer embedUrl={page.embedUrl} title={page.video.title} thumbnailUrl={page.video.thumbnailUrl} /></div>
      <h1>{page.video.title}</h1><div className="watch-meta"><Link to={`/channel/${page.video.channelId}`}>{page.video.channelTitle ?? 'Channel'}</Link><div className="watch-actions"><button className={page.reaction === 'like' ? 'active' : ''} onClick={() => void react('like')}><ThumbsUp /> Like</button><button className={page.reaction === 'dislike' ? 'active' : ''} onClick={() => void react('dislike')}><ThumbsDown /> Not for me</button><button className={subscribed ? 'active' : ''} onClick={() => void subscribe()}>{subscribed ? <Check /> : <Heart />} {subscribed ? 'Subscribed' : 'Subscribe'}</button><ShareAction url={page.shareUrl} title={page.video.title} /></div></div>
    </section>
    <aside className="up-next"><p className="eyebrow">KEEP WATCHING</p><h2>Watch next</h2><div className="related-list">{related.map(v => <VideoCard key={v.id} video={v} />)}</div>{discoveries.length > 0 && <><h2 className="explore-next">Explore next</h2><div className="related-list">{discoveries.map(item => <VideoCard key={item.video.id} video={item.video} reason={item.reason} locked pendingRequest={item.pendingRequest} />)}</div></>}</aside>
  </div>
}

function ShortsPage({ setError }: { setError: (s: string) => void }) {
  const [videos, setVideos] = useState<Video[]>([])
  const [session] = useState(() => crypto.randomUUID())
  const [loading, setLoading] = useState(false)
  const nextOffset = useRef(0)
  const loadingMore = useRef(false)
  const hasMore = useRef(true)
  const currentSession = useRef(session)
  useEffect(() => { currentSession.current = session }, [session])
  const loadMore = useCallback(async () => {
    if (loadingMore.current || !hasMore.current) return
    const requestedSession = session
    loadingMore.current = true
    setLoading(true)
    try {
      const page = await api.shorts(requestedSession, nextOffset.current, 8)
      if (currentSession.current !== requestedSession) return
      setVideos(current => [...current, ...page])
      nextOffset.current += page.length
      hasMore.current = page.length === 8
    } catch (cause) { setError(message(cause)) }
    finally {
      if (currentSession.current === requestedSession) {
        loadingMore.current = false
        setLoading(false)
      }
    }
  }, [session, setError])
  useEffect(() => {
    void Promise.resolve().then(() => {
      setVideos([])
      nextOffset.current = 0
      hasMore.current = true
      loadingMore.current = false
      return loadMore()
    })
  }, [loadMore])
  function blocked(index: number, notice: string) {
    setError(notice)
    setVideos(current => current.filter((_, candidate) => candidate !== index))
  }
  return <div className="shorts-wrap"><div className="shorts-feed">{videos.map((video, index) => <Short key={`${session}-${video.id}-${index}`} video={video} setError={setError} onActive={() => { if (index >= videos.length - 3) void loadMore() }} onBlocked={notice => blocked(index, notice)} />)}{loading && videos.length > 0 && <div className="shorts-loading"><LoaderCircle className="spin" /> Mixing more Shorts…</div>}{!loading && videos.length === 0 && <EmptyState icon={<Sparkles />} title="No Shorts yet" text="Approved Shorts will appear here after your channels update." />}</div></div>
}

function Short({ video, setError, onActive, onBlocked }: { video: Video; setError: (s: string) => void; onActive: () => void; onBlocked: (notice: string) => void }) {
  const root = useRef<HTMLElement>(null), [active, setActive] = useState(false)
  const becameActive = useEffectEvent(onActive)
  const [page, setPage] = useState<WatchData | null>(null), [reaction, setReaction] = useState<'like' | 'dislike' | undefined>()
  usePlaybackLease({ videoId: video.id, active: active && page !== null, onBlocked, onError: setError })
  useEffect(() => {
    const node = root.current
    if (!node) return
    const observer = new IntersectionObserver(([entry]) => setActive(entry.isIntersecting && entry.intersectionRatio > .65), { threshold: [.65] })
    observer.observe(node)
    return () => observer.disconnect()
  }, [])
  useEffect(() => {
    if (!active) return
    let cancelled = false
    becameActive()
    api.watch(video.id).then(data => {
      if (cancelled) return
      setPage(data)
      setReaction(data.reaction)
    }).catch(c => { if (!cancelled) setError(message(c)) })
    return () => { cancelled = true }
  }, [active, video.id, setError])
  async function react(kind: 'like' | 'dislike') { const next = reaction === kind ? undefined : kind; try { await api.react(video.id, next); setReaction(next) } catch (c) { setError(message(c)) } }
  return <section className="short" ref={root}><div className="short-player">{active && page ? <YouTubePlayer embedUrl={page.embedUrl} title={video.title} thumbnailUrl={video.thumbnailUrl} short /> : <div className="short-artwork" style={video.thumbnailUrl ? { backgroundImage: `url(${video.thumbnailUrl})` } : undefined}><LoaderCircle className="spin" /></div>}</div>{active && <><div className="short-caption"><Link to={`/channel/${video.channelId}`}>{video.channelTitle ?? 'Channel'}</Link><strong>{video.title}</strong></div><div className="short-actions"><button className={reaction === 'like' ? 'active' : ''} onClick={() => void react('like')}><ThumbsUp /><span>Like</span></button><button className={reaction === 'dislike' ? 'active' : ''} onClick={() => void react('dislike')}><ThumbsDown /><span>Not for me</span></button>{page && <ShareAction url={page.shareUrl} title={video.title} compact />}</div></>}</section>
}

function RequestsPage({ setError }: { setError: (s: string) => void }) {
  const [requests, setRequests] = useState<ChildRequest[]>([]), [loading, setLoading] = useState(true)
  const load = useCallback(() => { setLoading(true); return api.requests().then(setRequests).catch(c => setError(message(c))).finally(() => setLoading(false)) }, [setError])
  useEffect(() => { void Promise.resolve().then(load) }, [load])
  return <><PageHeading eyebrow="YOUR QUESTIONS" title="My asks" />{loading ? <ShelfSkeletons /> : requests.length ? <div className="request-list">{requests.map(r => <article key={r.id}><div className="channel-avatar">{r.channel.thumbnailUrl ? <img src={r.channel.thumbnailUrl} alt="" /> : r.channel.title.slice(0, 1)}</div><div><strong>{r.channel.title}</strong><small>Asked {relativeDate(r.createdAt)}</small></div><span className={`status ${r.status}`}>{requestLabel(r.status)}</span></article>)}</div> : <EmptyState icon={<Bell />} title="No asks yet" text="When you ask for a locked channel, its answer will show up here." />}</>
}

function PageHeading({ eyebrow, title, action }: { eyebrow: string; title: string; action?: React.ReactNode }) { return <header className="page-heading"><div><p className="eyebrow">{eyebrow}</p><h1>{title}</h1></div>{action}</header> }
function ShareAction({ url, title, compact = false }: { url: string; title: string; compact?: boolean }) {
  async function share() {
    if (navigator.share) {
      try { await navigator.share({ title, url }) } catch (cause) { if (!(cause instanceof DOMException && cause.name === 'AbortError')) window.open(url, '_blank', 'noopener,noreferrer') }
      return
    }
    window.open(url, '_blank', 'noopener,noreferrer')
  }
  return <button onClick={() => void share()} aria-label={`Share ${title}`}><Share2 />{compact ? <span>Share</span> : ' Share'}</button>
}
function InlineError({ text }: { text: string }) { return <p className="inline-error" role="alert">{text}</p> }
function EmptyLine({ text }: { text: string }) { return <div className="empty-line"><span>{text}</span></div> }
function EmptyState({ icon, title, text }: { icon: React.ReactNode; title: string; text: string }) { return <div className="empty-state"><span>{icon}</span><h2>{title}</h2><p>{text}</p></div> }
function ShelfSkeletons() { return <div className="skeletons" aria-label="Loading"><div /><div /><div /><div /><div /><div /></div> }
function message(cause: unknown) { return cause instanceof Error ? cause.message : 'Coop could not finish that.' }
function duration(total: number) { const minutes = Math.floor(total / 60), seconds = total % 60; return `${minutes}:${seconds.toString().padStart(2, '0')}` }
function relativeDate(value: string) { const days = Math.floor((Date.now() - Date.parse(value)) / 86400000); return days < 1 ? 'today' : `${days} day${days === 1 ? '' : 's'} ago` }
function requestLabel(status: ChildRequest['status']) { return status === 'pending' ? 'Waiting' : status === 'approved' ? 'Approved' : 'Not this time' }
function browserName() { const ua = navigator.userAgent; if (ua.includes('Edg')) return 'Edge'; if (ua.includes('Firefox')) return 'Firefox'; if (ua.includes('Chrome')) return 'Chrome'; if (ua.includes('Safari')) return 'Safari'; return 'Browser' }
function platformName() { const ua = navigator.userAgent; if (ua.includes('Mac')) return 'Mac'; if (ua.includes('Windows')) return 'Windows'; if (ua.includes('Linux')) return 'Linux'; return 'computer' }

export default App
