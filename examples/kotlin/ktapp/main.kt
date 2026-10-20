data class Vec(val x: Double, val y: Double) {
    operator fun plus(o: Vec) = Vec(x + o.x, y + o.y)
}

open class Shape { open fun area(): Double = 0.0 }
class Circle(val r: Double) : Shape() { override fun area() = 3.14159 * r * r }
class Sq(val s: Double) : Shape() { override fun area() = s * s }

fun fib(n: Int): Int = if (n < 2) n else fib(n - 1) + fib(n - 2)

fun main() {
    var v = Vec(0.0, 0.0)
    for (i in 0 until 1000) v = v + Vec(1.0, 2.0)
    val shapes = listOf<Shape>(Circle(1.0), Sq(2.0), Circle(3.0))
    val total = shapes.sumOf { it.area() }
    val words = "the quick brown fox".split(" ").map { it.uppercase() }
    println("fib(25)=${fib(25)} v=$v total=$total words=$words")
}
