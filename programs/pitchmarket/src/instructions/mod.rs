pub mod combo;
pub mod market;
pub mod orders;
pub mod redeem;
pub mod resolve;
pub mod settle;
pub mod vault;

// Anchor's #[program] macro expands each instruction's `Context<XAccounts>`
// assuming `crate::__client_accounts_x_accounts` is reachable from the crate
// root (it has no way to know these structs live in submodules) — these glob
// re-exports are what makes that resolve. Handler fns are named `*_handler`
// (see each submodule) precisely so this glob doesn't collide with the
// `#[program] mod pitchmarket { ... }` dispatch functions of the same name.
pub use combo::*;
pub use market::*;
pub use orders::*;
pub use redeem::*;
pub use resolve::*;
pub use settle::*;
pub use vault::*;
