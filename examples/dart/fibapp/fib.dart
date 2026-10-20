int fib(int n) => n < 2 ? n : fib(n - 1) + fib(n - 2);

class Vec {
  final double x, y;
  Vec(this.x, this.y);
  Vec operator +(Vec o) => Vec(x + o.x, y + o.y);
}

void main() {
  var sw = 0;
  for (var i = 0; i < 30; i++) sw += fib(i);
  var v = Vec(0, 0);
  for (var i = 0; i < 1000; i++) v = v + Vec(1, 2);
  print('fib sum $sw, vec ${v.x} ${v.y}');
}
