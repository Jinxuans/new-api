/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { ExternalLinkIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'

import { Turnstile } from '@/components/turnstile'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'

import type { TaskRewardClaim } from '../hooks/use-task-reward-claim'

type TaskRewardClaimDialogsProps = {
  claim: TaskRewardClaim
}

export function TaskRewardClaimDialogs(props: TaskRewardClaimDialogsProps) {
  const { t } = useTranslation()
  const claim = props.claim
  return (
    <>
      <Dialog
        open={Boolean(claim.passwordItem)}
        onOpenChange={(open) => {
          if (!open) claim.closePassword()
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('Enter task password')}</DialogTitle>
            <DialogDescription>
              {t(
                'Complete the community task, then enter its password to claim the reward.'
              )}
            </DialogDescription>
          </DialogHeader>
          <form
            onSubmit={claim.passwordForm.handleSubmit(claim.claimWithPassword)}
          >
            <FieldGroup>
              {claim.passwordItem?.action_url || claim.docsLink ? (
                <Button
                  type='button'
                  variant='outline'
                  nativeButton={false}
                  render={
                    <a
                      href={claim.passwordItem?.action_url || claim.docsLink}
                      target='_blank'
                      rel='noreferrer'
                    />
                  }
                >
                  <HugeiconsIcon icon={ExternalLinkIcon} strokeWidth={2} />
                  {t('Open community link')}
                </Button>
              ) : null}
              <Field
                data-invalid={Boolean(
                  claim.passwordForm.formState.errors.password
                )}
              >
                <FieldLabel htmlFor='promotion-task-password'>
                  {t('Task password')}
                </FieldLabel>
                <Input
                  id='promotion-task-password'
                  autoFocus
                  aria-invalid={Boolean(
                    claim.passwordForm.formState.errors.password
                  )}
                  {...claim.passwordForm.register('password')}
                />
                <FieldError
                  errors={[claim.passwordForm.formState.errors.password]}
                />
              </Field>
              <DialogFooter>
                <Button
                  type='button'
                  variant='outline'
                  disabled={claim.isPending}
                  onClick={claim.closePassword}
                >
                  {t('Cancel')}
                </Button>
                <Button type='submit' disabled={claim.isPending}>
                  {t('Claim reward')}
                </Button>
              </DialogFooter>
            </FieldGroup>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog
        open={Boolean(claim.turnstileItem)}
        onOpenChange={(open) => {
          if (!open) claim.closeTurnstile()
        }}
      >
        {claim.turnstileItem ? (
          <DialogContent>
            <DialogHeader>
              <DialogTitle>{t('Security check')}</DialogTitle>
              <DialogDescription>
                {t('Complete the security check to claim this reward.')}
              </DialogDescription>
            </DialogHeader>
            <div className='flex justify-center py-4'>
              <Turnstile
                key={claim.turnstileKey}
                siteKey={claim.turnstileSiteKey}
                onVerify={claim.claimAfterTurnstile}
                onExpire={claim.resetTurnstile}
              />
            </div>
          </DialogContent>
        ) : null}
      </Dialog>
    </>
  )
}
