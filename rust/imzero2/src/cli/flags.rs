use std::{fmt::Debug, str::FromStr};

pub fn find_flag_default<'a,I>(args: I, used: &mut roaring::RoaringBitmap,flag: &str,default_value: String) -> String
where
  I: IntoIterator<Item = &'a String>,
 {
    let mut found = false;

    for (i,arg) in args.into_iter().enumerate() {
        let i32 = i as u32;
        if found {
            used.insert_range(i32-1..i32+1);
            return arg.clone();
        }
        found = !used.contains(i32) && arg == flag;
    }
    if found {
        tracing::error!(flag=flag,"expecting argument for flag");
        panic!("expecting argument for flag")
    }
    return default_value;
}
pub fn find_flag_value_default_parsable<'a,I,T>(args: I, used: &mut roaring::RoaringBitmap, flag: &str, default_value: T) -> T
where
  I: IntoIterator<Item = &'a String>,
  T: FromStr+ToString+Debug,
  <T as FromStr>::Err: ToString+Debug
{
    let d = default_value.to_string();
    let v = find_flag_default(args, used, flag, d);
    let r = v.parse::<T>();
    if r.is_err() {
        let err = r.unwrap_err().to_string();
        tracing::error!(err=err,flag=flag,value=v,"unable to parse as float");
        panic!("unable to parse as float");
    }
    return r.unwrap();
}
pub fn find_flag_value_default_bool<'a,I>(args: I, used: &mut roaring::RoaringBitmap, flag: &str, default_value: bool) -> bool
where
  I: IntoIterator<Item = &'a String>
{
    let on = String::from("on");
    let off = String::from("off");
    let v = find_flag_default(args, used, flag, if default_value { on } else { off });
    return v == "on";
}
pub fn validate_all_args_used<'a,I>(args: I, argc: u32, used: &roaring::RoaringBitmap)
where
  I: IntoIterator<Item = &'a String>
{
    if used.contains_range(0..argc) {
        return
    }
    for (pos, arg) in args.into_iter().enumerate() {
        if !used.contains(pos as u32) {
            tracing::error!(arg=arg,pos=pos,"argument has not been used");
        }
    }
    panic!("not all arguments have been used");
}