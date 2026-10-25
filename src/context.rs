//! 请求上下文原语：时钟抽象与请求标识。
//!
//! 客户端 IP 提取待代理拓扑（直连 / 反代层数）确定后再定信任边界，
//! 不提前按假设实现。

use std::fmt;
use std::sync::atomic::{AtomicI64, Ordering};

use chrono::{DateTime, TimeDelta, Utc};
use uuid::Uuid;

/// 时间源。所有过期判断（token、授权码、验证码、封禁）必须经它，
/// 不得散落 `Utc::now()`，否则 §8 的不变量测试无法固定时间编写。
pub trait Clock: Send + Sync {
    /// 返回当前时刻。
    fn now(&self) -> DateTime<Utc>;
}

/// 真实时钟，生产装配用。
///
/// ```
/// use nebula_server::context::{Clock, SystemClock};
///
/// let clock = SystemClock;
/// assert!(clock.now().timestamp() > 1_700_000_000);
/// ```
#[derive(Debug, Clone, Copy, Default)]
pub struct SystemClock;

impl Clock for SystemClock {
    fn now(&self) -> DateTime<Utc> {
        Utc::now()
    }
}

/// 可拨动的假时钟，测试专用；内部以微秒计数存储，无锁。
///
/// ```
/// use chrono::{TimeDelta, Utc};
/// use nebula_server::context::{Clock, ManualClock};
///
/// let clock = ManualClock::new(Utc::now());
/// clock.advance(TimeDelta::seconds(60));
/// assert!(clock.now() > Utc::now());
/// ```
///
/// # Panics
/// 多次累计拨动使微秒计数超出 `DateTime` 可表示范围时，`now` 会 panic；
/// 测试中的常规时长不会触及该范围。
#[derive(Debug, Default)]
pub struct ManualClock {
    micros: AtomicI64,
}

impl ManualClock {
    /// 以给定起始时刻构建。
    #[must_use]
    pub fn new(start: DateTime<Utc>) -> Self {
        Self { micros: AtomicI64::new(start.timestamp_micros()) }
    }

    /// 向前拨动指定时长；超出 i64 微秒表示范围的时长饱和到边界值。
    pub fn advance(&self, by: TimeDelta) {
        // None 仅在 |微秒| 超出 i64 时出现，饱和语义即取边界
        self.micros.fetch_add(by.num_microseconds().unwrap_or(i64::MAX), Ordering::Relaxed);
    }
}

impl Clock for ManualClock {
    fn now(&self) -> DateTime<Utc> {
        DateTime::from_timestamp_micros(self.micros.load(Ordering::Relaxed))
            // 计数只经 new/advance 写入，越界对应上方 # Panics 条款
            .unwrap_or_else(|| unreachable!("微秒计数越界，见类型文档 # Panics"))
    }
}

/// 请求标识，uuid v7：时间有序，日志按字面排序即按时间排序。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct RequestId(Uuid);

impl RequestId {
    /// 生成新标识。
    ///
    /// ```
    /// use nebula_server::context::RequestId;
    ///
    /// let id = RequestId::new();
    /// assert_eq!(id.to_string().len(), 36);
    /// ```
    #[must_use]
    pub fn new() -> Self {
        Self(Uuid::now_v7())
    }
}

impl Default for RequestId {
    fn default() -> Self {
        Self::new()
    }
}

impl fmt::Display for RequestId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        self.0.fmt(f)
    }
}

#[cfg(test)]
mod tests {
    // 断言失败即 panic 是测试的正确失败语义；expect 禁令面向生产路径
    #![allow(clippy::expect_used)]

    use super::{Clock, ManualClock, RequestId, SystemClock};
    use chrono::{DateTime, TimeDelta, Utc};

    #[test]
    fn manual_clock_returns_exact_advanced_time() {
        // 给定：固定起点 2026-10-25T00:00:00Z
        let base = DateTime::parse_from_rfc3339("2026-10-25T00:00:00Z")
            .expect("固定字面量必须可解析")
            .with_timezone(&Utc);
        let clock = ManualClock::new(base);

        // 当：前拨 30 分钟
        clock.advance(TimeDelta::minutes(30));

        // 则：now 即起点 + 30 分钟，分毫不差
        assert_eq!(
            clock.now(),
            DateTime::parse_from_rfc3339("2026-10-25T00:30:00Z")
                .expect("固定字面量必须可解析")
                .with_timezone(&Utc)
        );
    }

    #[test]
    fn manual_clock_is_shareable_while_advancing() {
        // 作用域线程证明 Clock 可跨线程共享（trait 约束 Send + Sync 的实际意义）
        let clock = ManualClock::new(Utc::now());
        std::thread::scope(|scope| {
            scope.spawn(|| clock.advance(TimeDelta::minutes(1)));
        });

        assert!(clock.now() > Utc::now());
    }

    #[test]
    fn system_clock_does_not_go_backwards() {
        let clock = SystemClock;
        let first = clock.now();
        let second = clock.now();
        assert!(second >= first);
    }

    #[test]
    fn request_ids_are_distinct_and_ordered() {
        // v7 高位是毫秒时间戳；同进程相邻两次生成，字面序应非降
        let first = RequestId::new();
        let second = RequestId::new();
        assert_ne!(first, second);
        assert!(first.to_string() < second.to_string());
    }
}
