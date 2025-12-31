import { useReducer, useEffect } from 'react'
import { apiClient, ConfigResponse } from '../api/client'

// State types
interface FormState {
  port: number
  lanEnabled: boolean
  discordBatchSec: number
  notifyOnJoin: boolean
  notifyOnLeave: boolean
  notifyOnWorldJoin: boolean
  discordWebhookUrl: string
  logPath: string
  newPassword: string
}

interface SettingsState {
  config: ConfigResponse | null
  loading: boolean
  saving: boolean
  error: string | null
  success: string | null
  form: FormState
}

// Action types
type SettingsAction =
  | { type: 'LOAD_START' }
  | { type: 'LOAD_SUCCESS'; payload: ConfigResponse }
  | { type: 'LOAD_ERROR'; payload: string }
  | { type: 'SAVE_START' }
  | { type: 'SAVE_SUCCESS'; payload: string }
  | { type: 'SAVE_ERROR'; payload: string }
  | { type: 'UPDATE_FORM'; payload: Partial<FormState> }
  | { type: 'CLEAR_MESSAGES' }

const initialFormState: FormState = {
  port: 8080,
  lanEnabled: false,
  discordBatchSec: 3,
  notifyOnJoin: true,
  notifyOnLeave: true,
  notifyOnWorldJoin: true,
  discordWebhookUrl: '',
  logPath: '',
  newPassword: '',
}

const initialState: SettingsState = {
  config: null,
  loading: true,
  saving: false,
  error: null,
  success: null,
  form: initialFormState,
}

function settingsReducer(state: SettingsState, action: SettingsAction): SettingsState {
  switch (action.type) {
    case 'LOAD_START':
      return { ...state, loading: true, error: null }
    case 'LOAD_SUCCESS':
      return {
        ...state,
        loading: false,
        config: action.payload,
        form: {
          ...state.form,
          port: action.payload.port,
          lanEnabled: action.payload.lan_enabled,
          discordBatchSec: action.payload.discord_batch_sec,
          notifyOnJoin: action.payload.notify_on_join,
          notifyOnLeave: action.payload.notify_on_leave,
          notifyOnWorldJoin: action.payload.notify_on_world_join,
          logPath: action.payload.log_path,
        },
      }
    case 'LOAD_ERROR':
      return { ...state, loading: false, error: action.payload }
    case 'SAVE_START':
      return { ...state, saving: true, error: null, success: null }
    case 'SAVE_SUCCESS':
      return {
        ...state,
        saving: false,
        success: action.payload,
        form: { ...state.form, discordWebhookUrl: '', newPassword: '' },
      }
    case 'SAVE_ERROR':
      return { ...state, saving: false, error: action.payload }
    case 'UPDATE_FORM':
      return { ...state, form: { ...state.form, ...action.payload } }
    case 'CLEAR_MESSAGES':
      return { ...state, error: null, success: null }
    default:
      return state
  }
}

function Settings() {
  const [state, dispatch] = useReducer(settingsReducer, initialState)
  const { config, loading, saving, error, success, form } = state

  useEffect(() => {
    dispatch({ type: 'LOAD_START' })
    apiClient
      .fetchConfig()
      .then((data) => dispatch({ type: 'LOAD_SUCCESS', payload: data }))
      .catch((err) => dispatch({ type: 'LOAD_ERROR', payload: err.message }))
  }, [])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    dispatch({ type: 'SAVE_START' })

    try {
      const req: Record<string, unknown> = {
        port: form.port,
        lan_enabled: form.lanEnabled,
        discord_batch_sec: form.discordBatchSec,
        notify_on_join: form.notifyOnJoin,
        notify_on_leave: form.notifyOnLeave,
        notify_on_world_join: form.notifyOnWorldJoin,
        log_path: form.logPath,
      }

      if (form.discordWebhookUrl) {
        req.discord_webhook_url = form.discordWebhookUrl
      }
      if (form.newPassword) {
        req.basic_auth_password = form.newPassword
      }

      const res = await apiClient.updateConfig(req)

      if (res.success) {
        let message = 'Settings saved. Restart required to apply changes.'
        if (res.new_port && res.new_port !== form.port) {
          message = `Settings saved. Restart required. New port: ${res.new_port}`
        }
        dispatch({ type: 'SAVE_SUCCESS', payload: message })
      }
    } catch (err) {
      dispatch({
        type: 'SAVE_ERROR',
        payload: err instanceof Error ? err.message : 'Unknown error',
      })
    }
  }

  const updateForm = (updates: Partial<FormState>) => {
    dispatch({ type: 'UPDATE_FORM', payload: updates })
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <div className="text-gray-500">Loading...</div>
      </div>
    )
  }

  return (
    <div className="max-w-lg mx-auto">
      <form onSubmit={handleSubmit} className="space-y-6">
        {/* Error/Success messages */}
        {error && (
          <div className="bg-red-50 border border-red-200 rounded-lg p-4">
            <p className="text-red-700">{error}</p>
          </div>
        )}
        {success && (
          <div className="bg-yellow-50 border border-yellow-200 rounded-lg p-4">
            <p className="text-yellow-700">{success}</p>
          </div>
        )}

        {/* Server Settings */}
        <div className="bg-white rounded-lg shadow p-4 space-y-4">
          <h2 className="text-lg font-semibold text-gray-800">Server Settings</h2>

          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">
              Port
            </label>
            <input
              type="number"
              min="1"
              max="65535"
              value={form.port}
              onChange={(e) => updateForm({ port: parseInt(e.target.value) || 8080 })}
              className="w-full px-3 py-2 border border-gray-300 rounded-md focus:ring-blue-500 focus:border-blue-500"
            />
            <p className="text-xs text-gray-500 mt-1">
              Port number (1-65535). Restart required after change.
            </p>
          </div>

          <div className="flex items-center gap-3">
            <input
              type="checkbox"
              id="lanEnabled"
              checked={form.lanEnabled}
              onChange={(e) => updateForm({ lanEnabled: e.target.checked })}
              className="h-4 w-4 text-blue-600 focus:ring-blue-500 border-gray-300 rounded"
            />
            <label htmlFor="lanEnabled" className="text-sm text-gray-700">
              Enable LAN access (requires authentication)
            </label>
          </div>

          {/* LAN Mode Warning */}
          {form.lanEnabled && (
            <div className="bg-amber-50 border border-amber-200 rounded-lg p-3">
              <div className="flex items-start gap-2">
                <svg
                  className="w-5 h-5 text-amber-600 flex-shrink-0 mt-0.5"
                  fill="none"
                  stroke="currentColor"
                  viewBox="0 0 24 24"
                >
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    strokeWidth={2}
                    d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"
                  />
                </svg>
                <div className="text-sm">
                  <p className="font-medium text-amber-800">
                    LAN Mode Warning / LANモード警告
                  </p>
                  <p className="text-amber-700 mt-1">
                    Devices on your local network can access this app. Only enable on trusted networks.
                  </p>
                  <p className="text-amber-700 mt-1">
                    同じネットワーク内の端末からアクセスできます。信頼できるネットワーク以外では有効にしないでください。
                  </p>
                </div>
              </div>
            </div>
          )}
        </div>

        {/* Log Settings */}
        <div className="bg-white rounded-lg shadow p-4 space-y-4">
          <h2 className="text-lg font-semibold text-gray-800">Log Settings</h2>

          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">
              VRChat Log Path
            </label>
            <input
              type="text"
              value={form.logPath}
              onChange={(e) => updateForm({ logPath: e.target.value })}
              placeholder="Leave empty for auto-detect"
              className="w-full px-3 py-2 border border-gray-300 rounded-md focus:ring-blue-500 focus:border-blue-500 font-mono text-sm"
            />
            <p className="text-xs text-gray-500 mt-1">
              Path to VRChat log directory. Leave empty to auto-detect default location.
            </p>
          </div>
        </div>

        {/* Authentication Settings */}
        <div className="bg-white rounded-lg shadow p-4 space-y-4">
          <h2 className="text-lg font-semibold text-gray-800">Authentication</h2>

          {config?.basic_auth_configured ? (
            <div className="text-sm text-green-600 flex items-center gap-2">
              <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 20 20">
                <path
                  fillRule="evenodd"
                  d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z"
                  clipRule="evenodd"
                />
              </svg>
              Basic Auth configured (username: {config?.basic_auth_username || 'admin'})
            </div>
          ) : (
            <div className="text-sm text-yellow-600 flex items-center gap-2">
              <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 20 20">
                <path
                  fillRule="evenodd"
                  d="M8.257 3.099c.765-1.36 2.722-1.36 3.486 0l5.58 9.92c.75 1.334-.213 2.98-1.742 2.98H4.42c-1.53 0-2.493-1.646-1.743-2.98l5.58-9.92zM11 13a1 1 0 11-2 0 1 1 0 012 0zm-1-8a1 1 0 00-1 1v3a1 1 0 002 0V6a1 1 0 00-1-1z"
                  clipRule="evenodd"
                />
              </svg>
              No auth configured. Set password to enable LAN mode securely.
            </div>
          )}

          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">
              {config?.basic_auth_configured ? 'Change Password' : 'Set Password'}
            </label>
            <input
              type="password"
              value={form.newPassword}
              onChange={(e) => updateForm({ newPassword: e.target.value })}
              placeholder={config?.basic_auth_configured ? 'Enter new password to change' : 'Enter password'}
              className="w-full px-3 py-2 border border-gray-300 rounded-md focus:ring-blue-500 focus:border-blue-500"
            />
            <p className="text-xs text-gray-500 mt-1">
              Password for Basic Auth. Required when LAN mode is enabled.
            </p>
          </div>
        </div>

        {/* Discord Settings */}
        <div className="bg-white rounded-lg shadow p-4 space-y-4">
          <h2 className="text-lg font-semibold text-gray-800">Discord Notifications</h2>

          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">
              Webhook URL
            </label>
            <input
              type="url"
              value={form.discordWebhookUrl}
              onChange={(e) => updateForm({ discordWebhookUrl: e.target.value })}
              placeholder={
                config?.discord_webhook_configured
                  ? '(Webhook configured - enter new URL to change)'
                  : 'https://discord.com/api/webhooks/...'
              }
              className="w-full px-3 py-2 border border-gray-300 rounded-md focus:ring-blue-500 focus:border-blue-500"
            />
            {config?.discord_webhook_configured && (
              <p className="text-xs text-green-600 mt-1">Webhook is configured</p>
            )}
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">
              Batch Interval (seconds)
            </label>
            <input
              type="number"
              min="0"
              value={form.discordBatchSec}
              onChange={(e) => updateForm({ discordBatchSec: parseInt(e.target.value) || 0 })}
              className="w-full px-3 py-2 border border-gray-300 rounded-md focus:ring-blue-500 focus:border-blue-500"
            />
            <p className="text-xs text-gray-500 mt-1">
              Delay before sending notifications (0 = immediate)
            </p>
          </div>

          <div className="space-y-2">
            <p className="text-sm font-medium text-gray-700">Notify on:</p>
            <div className="flex items-center gap-3">
              <input
                type="checkbox"
                id="notifyOnJoin"
                checked={form.notifyOnJoin}
                onChange={(e) => updateForm({ notifyOnJoin: e.target.checked })}
                className="h-4 w-4 text-blue-600 focus:ring-blue-500 border-gray-300 rounded"
              />
              <label htmlFor="notifyOnJoin" className="text-sm text-gray-700">
                Player join
              </label>
            </div>
            <div className="flex items-center gap-3">
              <input
                type="checkbox"
                id="notifyOnLeave"
                checked={form.notifyOnLeave}
                onChange={(e) => updateForm({ notifyOnLeave: e.target.checked })}
                className="h-4 w-4 text-blue-600 focus:ring-blue-500 border-gray-300 rounded"
              />
              <label htmlFor="notifyOnLeave" className="text-sm text-gray-700">
                Player leave
              </label>
            </div>
            <div className="flex items-center gap-3">
              <input
                type="checkbox"
                id="notifyOnWorldJoin"
                checked={form.notifyOnWorldJoin}
                onChange={(e) => updateForm({ notifyOnWorldJoin: e.target.checked })}
                className="h-4 w-4 text-blue-600 focus:ring-blue-500 border-gray-300 rounded"
              />
              <label htmlFor="notifyOnWorldJoin" className="text-sm text-gray-700">
                World join
              </label>
            </div>
          </div>
        </div>

        {/* Save button */}
        <button
          type="submit"
          disabled={saving}
          className="w-full py-2 px-4 bg-blue-600 text-white font-medium rounded-md hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2 disabled:opacity-50"
        >
          {saving ? 'Saving...' : 'Save Settings'}
        </button>

        <p className="text-xs text-center text-gray-500">
          Changes require application restart to take effect.
        </p>
      </form>
    </div>
  )
}

export default Settings
