import { useState, useEffect } from 'react'
import { apiClient, ConfigResponse } from '../api/client'

function Settings() {
  const [config, setConfig] = useState<ConfigResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState<string | null>(null)

  // Form state
  const [port, setPort] = useState(8080)
  const [lanEnabled, setLanEnabled] = useState(false)
  const [discordBatchSec, setDiscordBatchSec] = useState(3)
  const [notifyOnJoin, setNotifyOnJoin] = useState(true)
  const [notifyOnLeave, setNotifyOnLeave] = useState(true)
  const [notifyOnWorldJoin, setNotifyOnWorldJoin] = useState(true)
  const [discordWebhookUrl, setDiscordWebhookUrl] = useState('')

  useEffect(() => {
    apiClient
      .fetchConfig()
      .then((data) => {
        setConfig(data)
        setPort(data.port)
        setLanEnabled(data.lan_enabled)
        setDiscordBatchSec(data.discord_batch_sec)
        setNotifyOnJoin(data.notify_on_join)
        setNotifyOnLeave(data.notify_on_leave)
        setNotifyOnWorldJoin(data.notify_on_world_join)
        setLoading(false)
      })
      .catch((err) => {
        setError(err.message)
        setLoading(false)
      })
  }, [])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setSaving(true)
    setError(null)
    setSuccess(null)

    try {
      const req: Record<string, unknown> = {
        port,
        lan_enabled: lanEnabled,
        discord_batch_sec: discordBatchSec,
        notify_on_join: notifyOnJoin,
        notify_on_leave: notifyOnLeave,
        notify_on_world_join: notifyOnWorldJoin,
      }

      // Only include webhook URL if changed
      if (discordWebhookUrl) {
        req.discord_webhook_url = discordWebhookUrl
      }

      const res = await apiClient.updateConfig(req)

      if (res.success) {
        setSuccess('Settings saved. Restart required to apply changes.')
        setDiscordWebhookUrl('') // Clear webhook URL field after save
        if (res.new_port && res.new_port !== port) {
          setSuccess(
            `Settings saved. Restart required. New port: ${res.new_port}`
          )
        }
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    } finally {
      setSaving(false)
    }
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
              value={port}
              onChange={(e) => setPort(parseInt(e.target.value) || 8080)}
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
              checked={lanEnabled}
              onChange={(e) => setLanEnabled(e.target.checked)}
              className="h-4 w-4 text-blue-600 focus:ring-blue-500 border-gray-300 rounded"
            />
            <label htmlFor="lanEnabled" className="text-sm text-gray-700">
              Enable LAN access (requires authentication)
            </label>
          </div>

          {/* LAN Mode Warning */}
          {lanEnabled && (
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

        {/* Discord Settings */}
        <div className="bg-white rounded-lg shadow p-4 space-y-4">
          <h2 className="text-lg font-semibold text-gray-800">Discord Notifications</h2>

          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">
              Webhook URL
            </label>
            <input
              type="url"
              value={discordWebhookUrl}
              onChange={(e) => setDiscordWebhookUrl(e.target.value)}
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
              value={discordBatchSec}
              onChange={(e) => setDiscordBatchSec(parseInt(e.target.value) || 0)}
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
                checked={notifyOnJoin}
                onChange={(e) => setNotifyOnJoin(e.target.checked)}
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
                checked={notifyOnLeave}
                onChange={(e) => setNotifyOnLeave(e.target.checked)}
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
                checked={notifyOnWorldJoin}
                onChange={(e) => setNotifyOnWorldJoin(e.target.checked)}
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
