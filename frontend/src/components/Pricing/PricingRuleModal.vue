<template>
  <BaseModal :open="open" :title="title" :close-label="t('pricingPage.actions.close')" variant="wide" @close="requestClose">
    <form ref="formRef" class="rule-form" @submit.prevent="submit">
      <div class="rule-intro">
        <div>
          <strong>{{ t('pricingPage.ruleModal.priorityTitle') }}</strong>
          <p>{{ t('pricingPage.ruleModal.priorityHint') }}</p>
        </div>
        <label class="enabled-field">
          <span>{{ t('pricingPage.ruleModal.enabled') }}</span>
          <span class="rule-switch">
            <input v-model="draft.enabled" type="checkbox" :disabled="busy" />
            <span aria-hidden="true"></span>
          </span>
        </label>
      </div>

      <div class="identity-grid">
        <label class="form-field" for="pricing-rule-name">
          <span>{{ t('pricingPage.ruleModal.name') }}</span>
          <input
            id="pricing-rule-name"
            v-model="draft.name"
            type="text"
            autocomplete="off"
            :disabled="busy"
            :placeholder="t('pricingPage.ruleModal.namePlaceholder')"
            :class="{ 'field-invalid': nameError }"
            :aria-invalid="Boolean(nameError)"
            aria-describedby="pricing-rule-name-error"
            @blur="touchIdentity('name')"
          />
          <small v-if="nameError" id="pricing-rule-name-error" class="inline-error">{{ nameError }}</small>
        </label>
        <label class="form-field pattern-field" for="pricing-rule-pattern">
          <span>{{ t('pricingPage.ruleModal.pattern') }}</span>
          <input
            id="pricing-rule-pattern"
            v-model="draft.pattern"
            type="text"
            spellcheck="false"
            autocomplete="off"
            :disabled="busy"
            :placeholder="t('pricingPage.ruleModal.patternPlaceholder')"
            :class="{ 'field-invalid': patternError }"
            :aria-invalid="Boolean(patternError)"
            aria-describedby="pricing-rule-pattern-help"
            @blur="touchIdentity('pattern')"
          />
          <small id="pricing-rule-pattern-help" :class="{ 'inline-error': patternError }">{{ patternError || t('pricingPage.ruleModal.patternHint') }}</small>
        </label>
      </div>

      <section class="rate-section" aria-labelledby="base-rate-title">
        <header>
          <div>
            <h3 id="base-rate-title">{{ t('pricingPage.ruleModal.baseRates') }}</h3>
            <p>{{ t('pricingPage.ruleModal.rateUnit') }}</p>
          </div>
        </header>
        <div class="rate-grid">
          <label v-for="field in rateFields" :key="field.key" class="form-field rate-field">
            <span>{{ t(field.labelKey) }}</span>
            <span class="number-input" :class="{ 'field-invalid': baseRateError(field.key) }">
              <span aria-hidden="true">$</span>
              <input
                v-model="draft.rates[field.key]"
                type="number"
                min="0"
                step="any"
                inputmode="decimal"
                :disabled="busy"
                :aria-invalid="Boolean(baseRateError(field.key))"
                :aria-describedby="`pricing-base-rate-${field.key}-error`"
                @blur="touchField(`base-${field.key}`)"
              />
            </span>
            <small v-if="baseRateError(field.key)" :id="`pricing-base-rate-${field.key}-error`" class="inline-error">{{ baseRateError(field.key) }}</small>
          </label>
        </div>
      </section>

      <section class="tier-section" aria-labelledby="tier-title">
        <header>
          <div>
            <h3 id="tier-title">{{ t('pricingPage.ruleModal.tiers') }}</h3>
            <p>{{ t('pricingPage.ruleModal.tierHint') }}</p>
          </div>
          <BaseButton type="button" variant="outline" :disabled="busy" @click="addTier">
            <Plus :size="15" />
            {{ t('pricingPage.ruleModal.addTier') }}
          </BaseButton>
        </header>

        <div v-if="draft.tiers.length" class="tier-list">
          <article v-for="(tier, index) in draft.tiers" :key="index" class="tier-card">
            <div class="tier-card-header">
              <div class="tier-identity">
                <span class="tier-index">{{ t('pricingPage.ruleModal.tierLabel', { index: index + 1 }) }}</span>
                <label class="form-field threshold-field">
                  <span>{{ t('pricingPage.ruleModal.threshold') }}</span>
                  <span class="number-input threshold-input" :class="{ 'field-invalid': tierThresholdError(index) }">
                    <input
                      v-model.number="tier.input_tokens_above"
                      type="number"
                      min="1"
                      step="1"
                      inputmode="numeric"
                      :disabled="busy"
                      :aria-invalid="Boolean(tierThresholdError(index))"
                      :aria-describedby="`pricing-tier-${index}-threshold-error`"
                      @blur="touchField(`tier-${index}-threshold`)"
                    />
                    <span>{{ t('pricingPage.ruleModal.tokens') }}</span>
                  </span>
                  <small v-if="tierThresholdError(index)" :id="`pricing-tier-${index}-threshold-error`" class="inline-error">{{ tierThresholdError(index) }}</small>
                </label>
              </div>
              <button
                type="button"
                class="icon-button action-btn danger"
                :disabled="busy"
                :aria-label="t('pricingPage.ruleModal.removeTier', { index: index + 1 })"
                :title="t('pricingPage.ruleModal.removeTier', { index: index + 1 })"
                @click="removeTier(index)"
              >
                <Trash2 :size="16" />
              </button>
            </div>
            <div class="rate-grid tier-rates">
              <label v-for="field in rateFields" :key="field.key" class="form-field rate-field">
                <span>{{ t(field.labelKey) }}</span>
                <span class="number-input" :class="{ 'field-invalid': tierRateError(index, field.key) }">
                  <span aria-hidden="true">$</span>
                  <input
                    v-model="tier.rates[field.key]"
                    type="number"
                    min="0"
                    step="any"
                    inputmode="decimal"
                    :disabled="busy"
                    :aria-invalid="Boolean(tierRateError(index, field.key))"
                    :aria-describedby="`pricing-tier-${index}-${field.key}-error`"
                    @blur="touchField(`tier-${index}-${field.key}`)"
                  />
                </span>
                <small v-if="tierRateError(index, field.key)" :id="`pricing-tier-${index}-${field.key}-error`" class="inline-error">{{ tierRateError(index, field.key) }}</small>
              </label>
            </div>
          </article>
        </div>
        <div v-else class="tier-empty">{{ t('pricingPage.ruleModal.noTiers') }}</div>
      </section>

      <div v-if="displayError" class="form-error" role="alert">
        <CircleAlert :size="17" />
        <span>{{ displayError }}</span>
      </div>

      <footer class="form-actions">
        <BaseButton type="button" variant="outline" :disabled="busy" @click="requestClose">
          {{ t('pricingPage.actions.cancel') }}
        </BaseButton>
        <BaseButton type="submit" :disabled="busy">
          <LoaderCircle v-if="busy" class="spin" :size="16" />
          <Save v-else :size="16" />
          {{ busy ? t('pricingPage.actions.saving') : t('pricingPage.actions.save') }}
        </BaseButton>
      </footer>
    </form>
  </BaseModal>
</template>

<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { CircleAlert, LoaderCircle, Plus, Save, Trash2 } from 'lucide-vue-next'
import type { PricingCustomRule, PricingRates, PricingTier } from '../../../bindings/codeswitch/services/models'
import BaseButton from '../common/BaseButton.vue'
import BaseModal from '../common/BaseModal.vue'
import { decimalMoney } from '../../utils/money'

type RateKey = keyof PricingRates
type EditableRates = Record<RateKey, string>
type EditableTier = { input_tokens_above: number; rates: EditableRates }

const props = withDefaults(defineProps<{
  open: boolean
  rule?: PricingCustomRule | null
  busy?: boolean
  error?: string
}>(), {
  rule: null,
  busy: false,
  error: '',
})

const emit = defineEmits<{
  (event: 'close'): void
  (event: 'save', rule: PricingCustomRule): void
}>()

const { t } = useI18n()
const formRef = ref<HTMLFormElement | null>(null)
const localError = ref('')
const nameTouched = ref(false)
const patternTouched = ref(false)
const submitted = ref(false)
const touchedFields = reactive<Record<string, boolean>>({})
const rateFields: Array<{ key: RateKey; labelKey: string }> = [
  { key: 'input', labelKey: 'pricingPage.rates.input' },
  { key: 'output', labelKey: 'pricingPage.rates.output' },
  { key: 'reasoning', labelKey: 'pricingPage.rates.reasoning' },
  { key: 'cache_read', labelKey: 'pricingPage.rates.cacheRead' },
  { key: 'cache_write', labelKey: 'pricingPage.rates.cacheWrite' },
]

const emptyRates = (): EditableRates => ({ input: '0', output: '0', reasoning: '0', cache_read: '0', cache_write: '0' })
const copyRates = (source?: Partial<PricingRates>): EditableRates => ({
  input: String(source?.input ?? '0'),
  output: String(source?.output ?? '0'),
  reasoning: String(source?.reasoning ?? '0'),
  cache_read: String(source?.cache_read ?? '0'),
  cache_write: String(source?.cache_write ?? '0'),
})

const draft = reactive<{
  id: string
  name: string
  pattern: string
  enabled: boolean
  order: number
  rates: EditableRates
  tiers: EditableTier[]
  created_at: string
  updated_at: string
}>({
  id: '',
  name: '',
  pattern: '',
  enabled: true,
  order: 0,
  rates: emptyRates(),
  tiers: [],
  created_at: '',
  updated_at: '',
})

const title = computed(() => props.rule?.id ? t('pricingPage.ruleModal.editTitle') : t('pricingPage.ruleModal.createTitle'))
const displayError = computed(() => localError.value || props.error)
const nameError = computed(() => (nameTouched.value || submitted.value) && !draft.name.trim() ? t('pricingPage.ruleModal.nameRequired') : '')
const patternError = computed(() => {
  if (!patternTouched.value && !submitted.value) return ''
  const pattern = draft.pattern.trim()
  if (!pattern) return t('pricingPage.ruleModal.patternRequired')
  try {
    new RegExp(pattern, 'i')
    return ''
  } catch (error) {
    return t('pricingPage.ruleModal.regexError', { message: error instanceof Error ? error.message : String(error) })
  }
})

const touchField = (key: string) => {
  touchedFields[key] = true
  localError.value = ''
}

const touchIdentity = (field: 'name' | 'pattern') => {
  if (field === 'name') nameTouched.value = true
  else patternTouched.value = true
  localError.value = ''
}

const shouldValidate = (key: string) => submitted.value || touchedFields[key]
const validRate = (value: unknown) => {
  if (value === '' || value === null || value === undefined) return false
  try { return decimalMoney(String(value)).gte(0) } catch { return false }
}

const baseRateError = (key: RateKey) => shouldValidate(`base-${key}`) && !validRate(draft.rates[key])
  ? t('pricingPage.ruleModal.rateFieldError')
  : ''

const tierThresholdError = (index: number) => {
  if (!shouldValidate(`tier-${index}-threshold`)) return ''
  const threshold = Number(draft.tiers[index]?.input_tokens_above)
  if (!Number.isSafeInteger(threshold) || threshold <= 0) return t('pricingPage.ruleModal.thresholdError')
  if (draft.tiers.some((tier, tierIndex) => tierIndex !== index && Number(tier.input_tokens_above) === threshold)) {
    return t('pricingPage.ruleModal.thresholdDuplicate')
  }
  return ''
}

const tierRateError = (index: number, key: RateKey) => shouldValidate(`tier-${index}-${key}`) && !validRate(draft.tiers[index]?.rates[key])
  ? t('pricingPage.ruleModal.rateFieldError')
  : ''

const focusFirstInvalid = () => nextTick(() => {
  formRef.value?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus()
})

const resetDraft = () => {
  const source = props.rule
  draft.id = source?.id || ''
  draft.name = source?.name || ''
  draft.pattern = source?.pattern || ''
  draft.enabled = source?.enabled ?? true
  draft.order = source?.order || 0
  draft.rates = copyRates(source?.rates)
  draft.tiers = (source?.tiers || []).map((tier: PricingTier) => ({
    input_tokens_above: Number(tier.input_tokens_above),
    rates: copyRates(tier.rates),
  }))
  draft.created_at = source?.created_at || ''
  draft.updated_at = source?.updated_at || ''
  localError.value = ''
  nameTouched.value = false
  patternTouched.value = false
  submitted.value = false
  for (const key of Object.keys(touchedFields)) delete touchedFields[key]
}

watch(() => [props.open, props.rule] as const, ([open]) => {
  if (open) resetDraft()
}, { immediate: true })

const requestClose = () => {
  if (!props.busy) emit('close')
}

const addTier = () => {
  const lastThreshold = draft.tiers.at(-1)?.input_tokens_above || 0
  draft.tiers.push({ input_tokens_above: lastThreshold + 100_000, rates: copyRates(draft.rates) })
}

const removeTier = (index: number) => {
  draft.tiers.splice(index, 1)
  for (const key of Object.keys(touchedFields)) {
    if (key.startsWith('tier-')) delete touchedFields[key]
  }
}

const normalizeRates = (rates: EditableRates): PricingRates => ({
  input: decimalMoney(rates.input).toFixed(),
  output: decimalMoney(rates.output).toFixed(),
  reasoning: decimalMoney(rates.reasoning).toFixed(),
  cache_read: decimalMoney(rates.cache_read).toFixed(),
  cache_write: decimalMoney(rates.cache_write).toFixed(),
}) as PricingRates

const ratesValid = (rates: EditableRates) => rateFields.every(({ key }) => validRate(rates[key]))

const submit = () => {
  localError.value = ''
  submitted.value = true
  const name = draft.name.trim()
  const pattern = draft.pattern.trim()
  nameTouched.value = true
  patternTouched.value = true
  if (nameError.value || patternError.value) {
    void focusFirstInvalid()
    return
  }
  if (!ratesValid(draft.rates)) {
    localError.value = t('pricingPage.ruleModal.rateError')
    void focusFirstInvalid()
    return
  }
  const thresholds = new Set<number>()
  for (const tier of draft.tiers) {
    const threshold = Number(tier.input_tokens_above)
    if (!Number.isSafeInteger(threshold) || threshold <= 0 || thresholds.has(threshold) || !ratesValid(tier.rates)) {
      localError.value = t('pricingPage.ruleModal.tierError')
      void focusFirstInvalid()
      return
    }
    thresholds.add(threshold)
  }

  emit('save', {
    id: draft.id,
    name,
    pattern,
    enabled: draft.enabled,
    order: draft.order,
    rates: normalizeRates(draft.rates),
    tiers: draft.tiers.map((tier) => ({
      input_tokens_above: Number(tier.input_tokens_above),
      rates: normalizeRates(tier.rates),
    } as PricingTier)),
    created_at: draft.created_at,
    updated_at: draft.updated_at,
  } as PricingCustomRule)
}
</script>

<style scoped>
.rule-form {
  display: grid;
  gap: 18px;
  min-width: 0;
}

.rule-intro {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 18px;
  padding: 13px 14px;
  border: 1px solid color-mix(in srgb, var(--mac-accent) 24%, var(--mac-border));
  border-radius: 9px;
  background: color-mix(in srgb, var(--mac-accent) 5%, var(--mac-surface));
}

.rule-intro strong { font-size: .9rem; }

.rule-intro p {
  max-width: 690px;
  margin: 4px 0 0;
  color: var(--mac-text-secondary);
  font-size: .8rem;
  line-height: 1.5;
  text-wrap: pretty;
}

.enabled-field {
  display: inline-flex;
  align-items: center;
  gap: 9px;
  flex: none;
  min-height: 34px;
  color: var(--mac-text);
  font-size: .82rem;
  font-weight: 600;
  cursor: pointer;
}

.rule-switch {
  position: relative;
  display: inline-flex;
  width: 42px;
  height: 24px;
  flex: none;
}

.rule-switch input {
  position: absolute;
  width: 1px;
  height: 1px;
  opacity: 0;
}

.rule-switch > span {
  width: 42px;
  height: 24px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--mac-text-secondary) 28%, transparent);
  transition: background-color .18s ease;
}

.rule-switch > span::after {
  content: '';
  display: block;
  width: 20px;
  height: 20px;
  margin: 2px;
  border-radius: 50%;
  background: var(--mac-toggle-thumb);
  box-shadow: 0 1px 3px color-mix(in srgb, var(--mac-text) 24%, transparent);
  transition: transform .18s ease;
}

.rule-switch input:checked + span { background: var(--mac-accent); }
.rule-switch input:checked + span::after { transform: translateX(18px); }
.rule-switch input:focus-visible + span { outline: 2px solid var(--mac-accent); outline-offset: 2px; }
.rule-switch input:disabled + span { opacity: .48; cursor: not-allowed; }

.identity-grid {
  display: grid;
  grid-template-columns: minmax(190px, .72fr) minmax(300px, 1.28fr);
  gap: 12px;
}

.form-field {
  display: grid;
  align-content: start;
  gap: 6px;
  min-width: 0;
}

.form-field > span:first-child {
  color: var(--mac-text-secondary);
  font-size: .76rem;
  font-weight: 600;
}

.form-field > input {
  width: 100%;
  min-width: 0;
  height: 40px;
  padding: 0 10px;
  border: 1px solid var(--mac-border);
  border-radius: 7px;
  background: var(--mac-surface-strong);
  color: var(--mac-text);
  font: inherit;
  font-size: .85rem;
  box-sizing: border-box;
}

.form-field > input:focus {
  outline: 2px solid color-mix(in srgb, var(--mac-accent) 42%, transparent);
  outline-offset: 1px;
  border-color: var(--mac-accent);
}

.form-field > input.field-invalid,
.number-input.field-invalid {
  border-color: var(--error);
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--error) 14%, transparent);
}

.form-field small {
  color: var(--mac-text-secondary);
  font-size: .72rem;
  line-height: 1.45;
}

.form-field small.inline-error { color: var(--error); }
.pattern-field > input { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; }

.number-input {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  align-items: center;
  min-width: 0;
  height: 40px;
  border: 1px solid var(--mac-border);
  border-radius: 7px;
  overflow: hidden;
  background: var(--mac-surface-strong);
  transition: border-color .18s ease, box-shadow .18s ease;
}

.number-input:focus-within {
  border-color: var(--mac-accent);
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--mac-accent) 18%, transparent);
}

.number-input > span {
  display: grid;
  place-items: center;
  min-width: 31px;
  height: 100%;
  border-right: 1px solid var(--mac-border);
  color: var(--mac-text-secondary);
  font: .72rem ui-monospace, SFMono-Regular, Consolas, monospace;
}

.number-input input {
  width: 100%;
  min-width: 0;
  height: 100%;
  padding: 0 9px;
  border: 0;
  outline: 0;
  background: transparent;
  color: var(--mac-text);
  font: .8rem ui-monospace, SFMono-Regular, Consolas, monospace;
  font-variant-numeric: tabular-nums;
  box-sizing: border-box;
}

.rate-section,
.tier-section {
  display: grid;
  gap: 13px;
  padding-top: 17px;
  border-top: 1px solid var(--mac-border);
}

.rate-section > header,
.tier-section > header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.rate-section h3,
.tier-section h3 {
  margin: 0;
  font-size: .94rem;
}

.rate-section p,
.tier-section p {
  max-width: 710px;
  margin: 4px 0 0;
  color: var(--mac-text-secondary);
  font-size: .77rem;
  line-height: 1.5;
  text-wrap: pretty;
}

.rate-grid {
  display: grid;
  grid-template-columns: repeat(5, minmax(105px, 1fr));
  align-items: start;
  gap: 10px;
}

.tier-list { display: grid; gap: 10px; }

.tier-card {
  display: grid;
  gap: 12px;
  padding: 12px;
  border: 1px solid var(--mac-border);
  border-radius: 8px;
  background: var(--mac-surface-strong);
}

.tier-card-header {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 12px;
}

.tier-identity {
  display: flex;
  align-items: flex-end;
  gap: 12px;
  min-width: 0;
}

.tier-index {
  display: grid;
  place-items: center;
  min-width: 58px;
  height: 40px;
  padding: 0 8px;
  border-radius: 7px;
  background: color-mix(in srgb, var(--mac-accent) 10%, var(--mac-surface));
  color: var(--mac-accent);
  font-size: .72rem;
  font-weight: 700;
  box-sizing: border-box;
}

.threshold-field { width: min(300px, 100%); }

.threshold-input { grid-template-columns: minmax(0, 1fr) auto; }

.threshold-input > span {
  min-width: auto;
  padding: 0 10px;
  border-right: 0;
  border-left: 1px solid var(--mac-border);
  font-family: inherit;
  white-space: nowrap;
}

.icon-button {
  display: inline-grid;
  place-items: center;
  width: 38px;
  height: 38px;
  padding: 0;
  border: 1px solid var(--mac-border);
  border-radius: 7px;
  background: var(--mac-surface);
  color: var(--mac-text-secondary);
  cursor: pointer;
  transition: border-color .18s ease, color .18s ease, background-color .18s ease, transform .12s ease;
}

.icon-button:hover:not(:disabled) {
  border-color: color-mix(in srgb, var(--error) 45%, var(--mac-border));
  background: color-mix(in srgb, var(--error) 5%, var(--mac-surface));
  color: var(--error);
}

.icon-button:active:not(:disabled) { transform: translateY(1px); }
.icon-button:focus-visible { outline: 2px solid var(--mac-accent); outline-offset: 2px; }
.icon-button:disabled { opacity: .45; cursor: not-allowed; }

.tier-empty {
  padding: 24px 18px;
  border: 1px dashed var(--mac-border);
  border-radius: 8px;
  background: var(--mac-surface-strong);
  color: var(--mac-text-secondary);
  font-size: .8rem;
  text-align: center;
}

.form-error {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 10px 12px;
  border: 1px solid color-mix(in srgb, var(--error) 35%, var(--mac-border));
  border-radius: 8px;
  background: color-mix(in srgb, var(--error) 6%, var(--mac-surface));
  color: var(--error);
  font-size: .8rem;
  line-height: 1.5;
}

.form-error svg { flex: none; margin-top: 1px; }

.form-actions {
  position: sticky;
  z-index: 2;
  bottom: -24px;
  display: flex;
  justify-content: flex-end;
  gap: 9px;
  margin: 0 -24px -24px;
  padding: 13px 24px;
  border-top: 1px solid var(--mac-border);
  background: color-mix(in srgb, var(--mac-surface) 95%, transparent);
  backdrop-filter: blur(8px);
}

.rule-form :deep(.btn) {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  min-height: 36px;
  padding: 0 13px !important;
  border-radius: 8px !important;
  white-space: nowrap;
}

.rule-form :deep(.btn-primary) { box-shadow: none; }
:deep(.modal-header .ghost-icon) { border-radius: 7px; background: var(--mac-surface-strong); }
:deep(.modal-header .ghost-icon:focus-visible) { outline: 2px solid var(--mac-accent); outline-offset: 2px; }
.spin { animation: pricing-spin .8s linear infinite; }

@keyframes pricing-spin { to { transform: rotate(360deg); } }

@media (prefers-reduced-motion: reduce) {
  .spin { animation-duration: 1.8s; }
  .rule-switch > span,
  .rule-switch > span::after,
  .number-input,
  .icon-button { transition: none; }
}

@media (max-width: 760px) {
  .rule-intro,
  .rate-section > header,
  .tier-section > header { align-items: stretch; flex-direction: column; }
  .enabled-field { align-self: flex-start; }
  .identity-grid { grid-template-columns: 1fr; }
  .rate-grid { grid-template-columns: repeat(2, minmax(110px, 1fr)); }
}

@media (max-width: 520px) {
  .tier-card-header,
  .tier-identity { align-items: stretch; flex-direction: column; }
  .tier-index { width: fit-content; }
  .threshold-field { width: 100%; }
  .icon-button { align-self: flex-end; }
}

@media (max-width: 460px) {
  .rate-grid { grid-template-columns: 1fr; }
  .form-actions { display: grid; grid-template-columns: 1fr 1fr; }
  .form-actions :deep(.btn) { justify-content: center; }
}
</style>
