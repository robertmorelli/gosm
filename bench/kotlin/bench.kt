import kotlin.time.TimeSource

class Node(val left: Node?, val right: Node?)

fun bottomUp(depth: Int): Node =
    if (depth <= 0) Node(null, null) else Node(bottomUp(depth - 1), bottomUp(depth - 1))

fun check(n: Node): Int = 1 + (n.left?.let { check(it) } ?: 0) + (n.right?.let { check(it) } ?: 0)

fun binaryTrees(maxDepth: Int): Long {
    var total = 0L
    val long = bottomUp(maxDepth)
    var d = 4
    while (d <= maxDepth) {
        val iters = 1 shl (maxDepth - d + 4)
        for (i in 0 until iters) total += check(bottomUp(d))
        d += 2
    }
    return total + check(long)
}

class Body(var x: Double, var y: Double, var z: Double, var vx: Double, var vy: Double, var vz: Double, val m: Double)

fun nbody(steps: Int): Double {
    val bodies = arrayOf(
        Body(0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 39.47),
        Body(4.84, -1.16, -0.10, 0.606, 2.81, -0.02, 0.037),
        Body(8.34, 4.12, -0.4, -1.01, 1.82, 0.008, 0.011),
        Body(12.89, -15.11, -0.22, 1.08, 0.868, -0.01, 0.0017),
        Body(15.37, -25.9, 0.179, 0.979, 0.594, -0.034, 0.002))
    repeat(steps) {
        for (i in bodies.indices) {
            val a = bodies[i]
            for (j in i + 1 until bodies.size) {
                val b = bodies[j]
                val dx = a.x - b.x; val dy = a.y - b.y; val dz = a.z - b.z
                val d2 = dx * dx + dy * dy + dz * dz
                val mag = 0.01 / (d2 * kotlin.math.sqrt(d2))
                a.vx -= dx * b.m * mag; a.vy -= dy * b.m * mag; a.vz -= dz * b.m * mag
                b.vx += dx * a.m * mag; b.vy += dy * a.m * mag; b.vz += dz * a.m * mag
            }
        }
        for (b in bodies) { b.x += 0.01 * b.vx; b.y += 0.01 * b.vy; b.z += 0.01 * b.vz }
    }
    return bodies.sumOf { it.x + it.y + it.z }
}

interface Shape { fun area(): Double }
class Circle(val r: Double) : Shape { override fun area() = 3.14159 * r * r }
class Rect(val w: Double, val h: Double) : Shape { override fun area() = w * h }
class Tri(val b: Double, val h: Double) : Shape { override fun area() = 0.5 * b * h }

fun virtualCalls(n: Int): Double {
    val shapes = Array<Shape>(1000) { i -> when (i % 3) { 0 -> Circle(i.toDouble()); 1 -> Rect(i.toDouble(), 2.0); else -> Tri(i.toDouble(), 3.0) } }
    var sum = 0.0
    repeat(n) { for (s in shapes) sum += s.area() }
    return sum
}

fun stringsAndMaps(n: Int): Int {
    val m = HashMap<String, Int>()
    for (i in 0 until n) {
        val k = "key" + (i % 1000)
        m[k] = (m[k] ?: 0) + i
    }
    val sb = StringBuilder()
    for ((k, v) in m) if (v % 7 == 0) sb.append(k)
    return sb.length + m.size
}

fun fib(n: Int): Int = if (n < 2) n else fib(n - 1) + fib(n - 2)

fun <T> timed(name: String, f: () -> T) {
    val t = TimeSource.Monotonic.markNow()
    val r = f()
    println("$name ${t.elapsedNow().inWholeMicroseconds / 1000.0} ms result=$r")
}

fun main() {
    timed("binary-trees") { binaryTrees(16) }
    timed("nbody") { nbody(2_000_000) }
    timed("virtual-calls") { virtualCalls(20_000) }
    timed("strings-maps") { stringsAndMaps(2_000_000) }
    timed("fib") { fib(32) }
}
