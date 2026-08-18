// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { DeviceSessionAction } from './DeviceSessionAction'

describe('device pairing policy', () => {
  it('does not expose sign out when a parent locked re-pairing', () => {
    render(<DeviceSessionAction allowSelfUnpair={false} onLogout={vi.fn()} />)

    expect(screen.queryByRole('button', { name: /sign out/i })).not.toBeInTheDocument()
    expect(screen.getByText(/parent controls device pairing/i)).toBeInTheDocument()
  })

  it('allows sign out after a parent enables re-pairing', () => {
    const logout = vi.fn()
    render(<DeviceSessionAction allowSelfUnpair onLogout={logout} />)

    fireEvent.click(screen.getByRole('button', { name: /sign out/i }))
    expect(logout).toHaveBeenCalledOnce()
  })
})
