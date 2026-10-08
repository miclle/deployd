# Enforce statement coverage per library package, not just a blended total.
NR == 1 { next }
NF == 3 {
    package = $1
    sub(/:[0-9]+\.[0-9]+,[0-9]+\.[0-9]+$/, "", package)
    sub(/\/[^\/]+$/, "", package)
    statements[package] += $2
    if ($3 > 0) covered[package] += $2
    total += $2
}
END {
    if (total == 0) {
        print "Coverage profile has no statements."
        exit 1
    }
    for (package in statements) {
        threshold = package == "github.com/miclle/deployd" ? 95 : 90
        percent = 100 * covered[package] / statements[package]
        printf "%s: %.1f%% (minimum %d%%)\n", package, percent, threshold
        if (percent < threshold) failed = 1
    }
    exit failed
}
