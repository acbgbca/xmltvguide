// ── Configuration ────────────────────────────────────────────────────────────
//
// PX_PER_MIN controls the horizontal zoom level of the guide.
// ROW_HEIGHT must match the --row-height CSS custom property in style.css.
// Changing either value here is the only thing needed to adjust the layout.

export const CONFIG = {
    PX_PER_MIN:     4,    // pixels per minute → 4 = 240px/hr, 2 hours = 480px on screen
    ROW_HEIGHT:     54,   // px — must match --row-height in style.css
    LABEL_INTERVAL: 30,   // minutes between time-axis labels
    MINS_IN_DAY:    1440,
    get TOTAL_WIDTH() { return this.MINS_IN_DAY * this.PX_PER_MIN; },
};
