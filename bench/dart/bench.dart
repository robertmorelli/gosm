import 'dart:math' as math;

class Node {
  final Node? left, right;
  Node(this.left, this.right);
}

Node bottomUp(int depth) =>
    depth <= 0 ? Node(null, null) : Node(bottomUp(depth - 1), bottomUp(depth - 1));

int check(Node n) =>
    1 + (n.left == null ? 0 : check(n.left!)) + (n.right == null ? 0 : check(n.right!));

int binaryTrees(int maxDepth) {
  var total = 0;
  final long = bottomUp(maxDepth);
  for (var d = 4; d <= maxDepth; d += 2) {
    final iters = 1 << (maxDepth - d + 4);
    for (var i = 0; i < iters; i++) total += check(bottomUp(d));
  }
  return total + check(long);
}

class Body {
  double x, y, z, vx, vy, vz;
  final double m;
  Body(this.x, this.y, this.z, this.vx, this.vy, this.vz, this.m);
}

double nbody(int steps) {
  final bodies = [
    Body(0, 0, 0, 0, 0, 0, 39.47),
    Body(4.84, -1.16, -0.10, 0.606, 2.81, -0.02, 0.037),
    Body(8.34, 4.12, -0.4, -1.01, 1.82, 0.008, 0.011),
    Body(12.89, -15.11, -0.22, 1.08, 0.868, -0.01, 0.0017),
    Body(15.37, -25.9, 0.179, 0.979, 0.594, -0.034, 0.002),
  ];
  for (var s = 0; s < steps; s++) {
    for (var i = 0; i < bodies.length; i++) {
      final a = bodies[i];
      for (var j = i + 1; j < bodies.length; j++) {
        final b = bodies[j];
        final dx = a.x - b.x, dy = a.y - b.y, dz = a.z - b.z;
        final d2 = dx * dx + dy * dy + dz * dz;
        final mag = 0.01 / (d2 * math.sqrt(d2));
        a.vx -= dx * b.m * mag; a.vy -= dy * b.m * mag; a.vz -= dz * b.m * mag;
        b.vx += dx * a.m * mag; b.vy += dy * a.m * mag; b.vz += dz * a.m * mag;
      }
    }
    for (final b in bodies) { b.x += 0.01 * b.vx; b.y += 0.01 * b.vy; b.z += 0.01 * b.vz; }
  }
  var sum = 0.0;
  for (final b in bodies) sum += b.x + b.y + b.z;
  return sum;
}

abstract class Shape { double area(); }
class Circle implements Shape { final double r; Circle(this.r); double area() => 3.14159 * r * r; }
class Rect implements Shape { final double w, h; Rect(this.w, this.h); double area() => w * h; }
class Tri implements Shape { final double b, h; Tri(this.b, this.h); double area() => 0.5 * b * h; }

double virtualCalls(int n) {
  final shapes = List<Shape>.generate(1000, (i) => i % 3 == 0 ? Circle(i.toDouble()) : i % 3 == 1 ? Rect(i.toDouble(), 2) : Tri(i.toDouble(), 3));
  var sum = 0.0;
  for (var k = 0; k < n; k++) { for (final s in shapes) sum += s.area(); }
  return sum;
}

int stringsAndMaps(int n) {
  final m = <String, int>{};
  for (var i = 0; i < n; i++) {
    final k = 'key${i % 1000}';
    m[k] = (m[k] ?? 0) + i;
  }
  final sb = StringBuffer();
  m.forEach((k, v) { if (v % 7 == 0) sb.write(k); });
  return sb.length + m.length;
}

int fib(int n) => n < 2 ? n : fib(n - 1) + fib(n - 2);

void timed(String name, Object Function() f) {
  final sw = Stopwatch()..start();
  final r = f();
  print('$name ${sw.elapsedMicroseconds / 1000} ms result=$r');
}

void main() {
  timed('binary-trees', () => binaryTrees(16));
  timed('nbody', () => nbody(2000000));
  timed('virtual-calls', () => virtualCalls(20000));
  timed('strings-maps', () => stringsAndMaps(2000000));
  timed('fib', () => fib(32));
}
