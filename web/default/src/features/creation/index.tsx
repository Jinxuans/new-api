import { useEffect, useMemo, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { ExternalLink, KeyRound, PenLine } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { getApiKeys, fetchTokenKey } from '@/features/keys/api'
import type { ApiKey } from '@/features/keys/types'
import { useStatus } from '@/hooks/use-status'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

const DEFAULT_TOKEN_ID_KEY = 'creation_default_token_id'

function encodeBase64(value: string): string {
  return btoa(unescape(encodeURIComponent(value)))
}

function getServerAddress(): string {
  return window.location.origin
}

function buildCreationLink(template: string, key: string): string {
  const serverAddress = getServerAddress()
  const fullKey = key.startsWith('sk-') ? key : `sk-${key}`

  if (template.includes('{cherryConfig}')) {
    const cherryConfig = {
      id: 'new-api',
      baseUrl: serverAddress,
      apiKey: fullKey,
    }
    return template.replaceAll(
      '{cherryConfig}',
      encodeURIComponent(encodeBase64(JSON.stringify(cherryConfig)))
    )
  }

  if (template.includes('{aionuiConfig}')) {
    const aionuiConfig = {
      platform: 'new-api',
      baseUrl: serverAddress,
      apiKey: fullKey,
    }
    return template.replaceAll(
      '{aionuiConfig}',
      encodeURIComponent(encodeBase64(JSON.stringify(aionuiConfig)))
    )
  }

  return template
    .replaceAll('{address}', encodeURIComponent(serverAddress))
    .replaceAll('{key}', fullKey)
}

export function CreationLauncher() {
  const { t } = useTranslation()
  const { status } = useStatus()
  const [tokens, setTokens] = useState<ApiKey[]>([])
  const [selectedTokenId, setSelectedTokenId] = useState('')
  const [loading, setLoading] = useState(true)
  const [opening, setOpening] = useState(false)

  const creationLink = useMemo(() => {
    const value = (status as Record<string, unknown> | null)?.creation_link
    return typeof value === 'string' ? value.trim() : ''
  }, [status])

  useEffect(() => {
    let mounted = true

    async function loadTokens() {
      try {
        setLoading(true)
        const res = await getApiKeys({ p: 1, size: 100 })
        const items = res.success ? res.data?.items || [] : []
        const activeTokens = items.filter((item) => item.status === 1)

        if (!mounted) return
        setTokens(activeTokens)

        const savedId = localStorage.getItem(DEFAULT_TOKEN_ID_KEY)
        const savedToken = activeTokens.find(
          (item) => String(item.id) === savedId
        )
        setSelectedTokenId(String(savedToken?.id || activeTokens[0]?.id || ''))
      } catch {
        if (mounted) toast.error(t('Failed to load API keys'))
      } finally {
        if (mounted) setLoading(false)
      }
    }

    loadTokens()

    return () => {
      mounted = false
    }
  }, [t])

  const openCreation = async () => {
    if (!creationLink) {
      toast.error(t('Please ask an administrator to configure the creation link'))
      return
    }
    if (!selectedTokenId) {
      toast.error(t('Please select an API key'))
      return
    }

    const targetWindow = window.open('about:blank', '_blank')
    if (targetWindow) {
      targetWindow.opener = null
    }

    try {
      setOpening(true)
      localStorage.setItem(DEFAULT_TOKEN_ID_KEY, selectedTokenId)
      const res = await fetchTokenKey(Number(selectedTokenId))
      if (!res.success || !res.data?.key) {
        throw new Error(res.message || t('Failed to load API key'))
      }

      const url = buildCreationLink(creationLink, res.data.key)
      if (targetWindow && !targetWindow.closed) {
        targetWindow.location.href = url
      } else {
        window.open(url, '_blank', 'noopener,noreferrer')
      }
    } catch (error) {
      targetWindow?.close()
      toast.error(
        error instanceof Error ? error.message : t('Failed to open creation')
      )
    } finally {
      setOpening(false)
    }
  }

  return (
    <div className='mx-auto flex w-full max-w-3xl flex-1 items-center px-4 py-8'>
      <Card className='w-full'>
        <CardHeader>
          <div className='bg-muted mb-2 flex size-10 items-center justify-center rounded-lg'>
            <PenLine className='size-5' />
          </div>
          <CardTitle>{t('Creation')}</CardTitle>
          <CardDescription>
            {t('Open the configured creation app with one of your API keys.')}
          </CardDescription>
        </CardHeader>
        <CardContent className='space-y-4'>
          {!creationLink && (
            <div className='text-muted-foreground rounded-lg border p-3 text-sm'>
              {t(
                'Creation link is not configured. Administrators can set it in Content Settings.'
              )}
            </div>
          )}

          {tokens.length > 0 ? (
            <Select
              value={selectedTokenId}
              onValueChange={setSelectedTokenId}
              disabled={loading || opening}
            >
              <SelectTrigger>
                <SelectValue placeholder={t('Select an API key')} />
              </SelectTrigger>
              <SelectContent>
                {tokens.map((token) => (
                  <SelectItem key={token.id} value={String(token.id)}>
                    {token.name || `#${token.id}`}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : (
            <div className='text-muted-foreground rounded-lg border p-3 text-sm'>
              {loading
                ? t('Loading API keys...')
                : t('No active API keys are available. Create one first.')}
            </div>
          )}

          <div className='flex flex-wrap gap-2'>
            <Button
              type='button'
              onClick={openCreation}
              disabled={loading || opening || !selectedTokenId || !creationLink}
            >
              <ExternalLink className='size-4' />
              {opening ? t('Opening...') : t('Open Creation')}
            </Button>
            <Button asChild type='button' variant='outline'>
              <Link to='/keys'>
                <KeyRound className='size-4' />
                {t('Manage API Keys')}
              </Link>
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
