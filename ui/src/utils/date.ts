/**
 * 日期时间格式化工具函数
 */

/**
 * 解析日期字符串，支持多种格式
 * @param dateStr 日期字符串，支持 ISO 8601 格式或 YYYY-MM-DD HH:mm:ss 格式
 * @returns Date 对象，如果解析失败返回 null
 */
const parseDate = (dateStr: string | null | undefined): Date | null => {
  if (!dateStr) return null

  try {
    // 尝试直接解析（支持 ISO 8601 和 YYYY-MM-DD HH:mm:ss 格式）
    let date = new Date(dateStr)

    // 如果直接解析失败，尝试将空格替换为 T（兼容 YYYY-MM-DD HH:mm:ss 格式）
    if (isNaN(date.getTime()) && typeof dateStr === 'string' && dateStr.includes(' ')) {
      date = new Date(dateStr.replace(' ', 'T'))
    }

    if (isNaN(date.getTime())) return null
    return date
  } catch (error) {
    return null
  }
}

/**
 * 格式化日期时间，显示为完整格式
 * @param dateStr 日期字符串，支持 ISO 8601 或 YYYY-MM-DD HH:mm:ss 格式
 * @returns 格式化后的日期字符串，格式：YYYY-MM-DD HH:mm
 */
export const formatDateTime = (dateStr: string | null | undefined): string => {
  const date = parseDate(dateStr)
  if (!date) return '-'

  try {
    const year = date.getFullYear()
    const month = String(date.getMonth() + 1).padStart(2, '0')
    const day = String(date.getDate()).padStart(2, '0')
    const hours = String(date.getHours()).padStart(2, '0')
    const minutes = String(date.getMinutes()).padStart(2, '0')

    // 始终显示完整日期时间格式：YYYY-MM-DD HH:mm
    return `${year}-${month}-${day} ${hours}:${minutes}`
  } catch (error) {
    console.error('日期格式化失败:', error)
    return '-'
  }
}

/**
 * 格式化完整日期时间，始终显示年份
 * @param dateStr 日期字符串，支持 ISO 8601 或 YYYY-MM-DD HH:mm:ss 格式
 * @returns 格式化后的日期字符串，格式：YYYY-MM-DD HH:mm
 */
export const formatFullDateTime = (dateStr: string | null | undefined): string => {
  const date = parseDate(dateStr)
  if (!date) return '-'

  try {
    const year = date.getFullYear()
    const month = String(date.getMonth() + 1).padStart(2, '0')
    const day = String(date.getDate()).padStart(2, '0')
    const hours = String(date.getHours()).padStart(2, '0')
    const minutes = String(date.getMinutes()).padStart(2, '0')

    return `${year}-${month}-${day} ${hours}:${minutes}`
  } catch (error) {
    console.error('日期格式化失败:', error)
    return '-'
  }
}

/**
 * 格式化日期，只显示日期部分
 * @param dateStr 日期字符串，支持 ISO 8601 或 YYYY-MM-DD HH:mm:ss 格式
 * @returns 格式化后的日期字符串，格式：YYYY-MM-DD
 */
export const formatDate = (dateStr: string | null | undefined): string => {
  const date = parseDate(dateStr)
  if (!date) return '-'

  try {
    const year = date.getFullYear()
    const month = String(date.getMonth() + 1).padStart(2, '0')
    const day = String(date.getDate()).padStart(2, '0')

    return `${year}-${month}-${day}`
  } catch (error) {
    console.error('日期格式化失败:', error)
    return '-'
  }
}

/**
 * 格式化相对时间（如：2小时前、3天前）
 * @param dateStr 日期字符串，支持 ISO 8601 或 YYYY-MM-DD HH:mm:ss 格式
 * @returns 相对时间字符串
 */
export const formatRelativeTime = (dateStr: string | null | undefined): string => {
  const date = parseDate(dateStr)
  if (!date) return '-'

  try {
    const now = new Date()
    const diffMs = now.getTime() - date.getTime()
    const diffSeconds = Math.floor(diffMs / 1000)
    const diffMinutes = Math.floor(diffSeconds / 60)
    const diffHours = Math.floor(diffMinutes / 60)
    const diffDays = Math.floor(diffHours / 24)

    if (diffSeconds < 60) {
      return '刚刚'
    } else if (diffMinutes < 60) {
      return `${diffMinutes}分钟前`
    } else if (diffHours < 24) {
      return `${diffHours}小时前`
    } else if (diffDays < 7) {
      return `${diffDays}天前`
    } else {
      // 超过7天，显示具体日期
      return formatDateTime(dateStr)
    }
  } catch (error) {
    console.error('相对时间格式化失败:', error)
    return '-'
  }
}
